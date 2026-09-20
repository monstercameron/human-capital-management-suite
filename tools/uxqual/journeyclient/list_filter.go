package journeyclient

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// ActionFilterList is the submit id of the Journeys tracker's filter form
// (UXLIVE-031). Applying it is a navigation, never a read: the list is
// narrowed from the answers already in hand, and the address carries the
// state so a reload, a copied link and Back all reproduce it.
const ActionFilterList = "filter-journeys"

// Fragment query keys for the list filter. The product router carries the
// same state under productui's journey_* keys; ProductJourneyHref and
// JourneyFragment translate between the two spellings.
const (
	fragmentFilterQuery  = "q"
	fragmentFilterStatus = "status"
	fragmentFilterFrom   = "from"
	fragmentFilterTo     = "to"
	fragmentFilterSort   = "sort"
	fragmentFilterGroup  = "group"
)

// Field ids of the filter form's controls.
const (
	FieldFilterQuery  = "journey-filter-q"
	FieldFilterStatus = "journey-filter-status"
	FieldFilterFrom   = "journey-filter-from"
	FieldFilterTo     = "journey-filter-to"
	FieldFilterSort   = "journey-filter-sort"
	FieldFilterGroup  = "journey-filter-group"
)

// listFilterParam reads the list filter out of a fragment's query. A
// malformed query is an unfiltered list rather than an error: the fragment
// is a convenience address, and the product router has already refused a
// malformed product address before it ever produced one.
func listFilterParam(query string) productui.JourneyListFilter {
	values, err := url.ParseQuery(query)
	if err != nil {
		return productui.JourneyListFilter{}
	}
	return productui.NormalizeJourneyListFilter(productui.JourneyListFilter{
		Query: values.Get(fragmentFilterQuery), Status: values.Get(fragmentFilterStatus),
		From: values.Get(fragmentFilterFrom), To: values.Get(fragmentFilterTo),
		Sort: values.Get(fragmentFilterSort), Group: values.Get(fragmentFilterGroup),
	})
}

// listFilterQuery is listFilterParam's inverse, in a fixed key order so one
// state has one address.
func listFilterQuery(filter productui.JourneyListFilter) []string {
	filter = productui.NormalizeJourneyListFilter(filter)
	parts := make([]string, 0, 6)
	for _, pair := range []struct{ key, value string }{
		{fragmentFilterQuery, filter.Query}, {fragmentFilterStatus, filter.Status},
		{fragmentFilterFrom, filter.From}, {fragmentFilterTo, filter.To},
		{fragmentFilterSort, filter.Sort}, {fragmentFilterGroup, filter.Group},
	} {
		if pair.value != "" {
			parts = append(parts, pair.key+"="+url.QueryEscape(pair.value))
		}
	}
	return parts
}

// ListFilterHref is the list route with one filter applied.
func ListFilterHref(workerRef string, filter productui.JourneyListFilter) string {
	return Href(Route{Kind: RouteList, WorkerRef: strings.TrimSpace(workerRef), Filter: productui.NormalizeJourneyListFilter(filter)})
}

// ListFilterFromForm reads a submitted filter form. The form's control
// names are the product route keys, so a plain GET of the same form and the
// live submit land on the same state.
func ListFilterFromForm(values map[string]string) productui.JourneyListFilter {
	form := url.Values{}
	for key, value := range values {
		form.Set(key, value)
	}
	return productui.JourneyListFilterFromValues(form)
}

// filterJourneys narrows the already-authorized journeys to the filter and
// orders them by recency. It never adds a record and never consults
// authority: every journey here came from a ListJourneys answer the server
// already scoped to the viewer, and the filter only hides some of them.
func filterJourneys(journeys []*journeyv1.Journey, filter productui.JourneyListFilter) []*journeyv1.Journey {
	filter = productui.NormalizeJourneyListFilter(filter)
	result := make([]*journeyv1.Journey, 0, len(journeys))
	for _, j := range journeys {
		if j == nil || !journeyMatchesListFilter(j, filter) {
			continue
		}
		result = append(result, j)
	}
	oldest := filter.Sort == productui.JourneyListSortOldest
	sort.SliceStable(result, func(left, right int) bool {
		a, b := journeyActivity(result[left]), journeyActivity(result[right])
		if a.Equal(b) {
			return false
		}
		if oldest {
			return a.Before(b)
		}
		return a.After(b)
	})
	return result
}

func journeyMatchesListFilter(j *journeyv1.Journey, filter productui.JourneyListFilter) bool {
	switch filter.Status {
	case "":
	case productui.JourneyListStatusOpen:
		// Open is everything the server has not closed and the tracker does
		// not already file under closed requests.
		if JourneyClosed(j) || journeyGroup(stageOf(j.GetStage())) == journey.JourneyGroupClosed {
			return false
		}
	default:
		if string(journeyGroup(stageOf(j.GetStage()))) != filter.Status {
			return false
		}
	}
	if (filter.From != "" || filter.To != "") && !productui.JourneyListDateInRange(journeyActivityDate(j), filter.From, filter.To) {
		return false
	}
	return productui.MatchesJourneyListQuery(filter.Query,
		j.GetWorkerName(), j.GetWorkerRef(), journey.JourneyReference(j.GetIntentId()), j.GetIntentId())
}

// journeyActivity is the time a journey last changed: its update time, or
// its creation time when the wire carried no update. A journey with neither
// sorts as the oldest possible activity.
func journeyActivity(j *journeyv1.Journey) time.Time {
	if ts := j.GetUpdatedAt(); ts != nil && ts.IsValid() {
		return ts.AsTime()
	}
	if ts := j.GetCreatedAt(); ts != nil && ts.IsValid() {
		return ts.AsTime()
	}
	return time.Time{}
}

func journeyActivityDate(j *journeyv1.Journey) string {
	activity := journeyActivity(j)
	if activity.IsZero() {
		return ""
	}
	return activity.UTC().Format("2006-01-02")
}

// listFilterView projects the filter form and its result statement.
func listFilterView(copy productui.LocaleContext, data ListData, shown, total int) *journey.JourneyFilterView {
	filter := productui.NormalizeJourneyListFilter(data.Filter)
	statusOptions := []journey.Option{{Value: "", Label: copy.Text("journey.filter_status_all"), Selected: filter.Status == ""}}
	for _, status := range productui.JourneyListStatuses() {
		label := copy.Text("journey.filter_status_open")
		if status != productui.JourneyListStatusOpen {
			label = copy.Text("journey.group." + status)
		}
		statusOptions = append(statusOptions, journey.Option{Value: status, Label: label, Selected: filter.Status == status})
	}
	sortValue := filter.Sort
	if sortValue == "" {
		sortValue = productui.JourneyListSortRecent
	}
	groupValue := filter.Group
	if groupValue == "" {
		groupValue = productui.JourneyListGroupPerson
	}
	options := func(selected string, values []string, keyPrefix string) []journey.Option {
		result := make([]journey.Option, 0, len(values))
		for _, value := range values {
			result = append(result, journey.Option{Value: value, Label: copy.Text(keyPrefix + value), Selected: value == selected})
		}
		return result
	}
	view := &journey.JourneyFilterView{
		Label: copy.Text("journey.filter_region"),
		Fields: []journey.Field{
			{ID: FieldFilterQuery, Name: productui.JourneyListQueryKey, Kind: "text", Label: copy.Text("journey.filter_search"),
				Value: filter.Query, Placeholder: copy.Text("journey.filter_search_placeholder")},
			{ID: FieldFilterStatus, Name: productui.JourneyListStatusKey, Kind: "select", Label: copy.Text("journey.filter_status"),
				Value: filter.Status, Options: statusOptions},
			// Row one of the layout is search, status, order and grouping;
			// row two is the date range beside the actions.
			{ID: FieldFilterSort, Name: productui.JourneyListSortKey, Kind: "select", Label: copy.Text("journey.filter_sort"),
				Value: sortValue, Options: options(sortValue, productui.JourneyListSorts(), "journey.filter_sort_")},
			{ID: FieldFilterGroup, Name: productui.JourneyListGroupKey, Kind: "select", Label: copy.Text("journey.filter_group"),
				Value: groupValue, Options: options(groupValue, productui.JourneyListGroupings(), "journey.filter_group_")},
			{ID: FieldFilterFrom, Name: productui.JourneyListFromKey, Kind: "date", Label: copy.Text("journey.filter_from"), Value: filter.From},
			{ID: FieldFilterTo, Name: productui.JourneyListToKey, Kind: "date", Label: copy.Text("journey.filter_to"), Value: filter.To},
		},
		Submit:   copy.Text("history.apply"),
		Narrowed: filter.Narrowing(),
		Shown:    shown,
		Total:    total,
	}
	for _, set := range []bool{filter.Status != "", filter.Sort != "", filter.Group != "", filter.From != "", filter.To != ""} {
		if set {
			view.PanelActive++
		}
	}
	view.ToggleLabel = copy.Text("journey.filter_toggle")
	if view.PanelActive > 0 {
		view.ToggleLabel = copy.Text("journey.filter_toggle_count", map[string]string{"count": copy.FormatNumber(strconv.Itoa(view.PanelActive), 0)})
	}
	if view.Narrowed {
		view.Result = copy.Text("journey.filter_result", map[string]string{
			"shown": copy.FormatNumber(strconv.Itoa(shown), 0), "total": copy.FormatNumber(strconv.Itoa(total), 0),
		})
	} else if total == 1 {
		view.Result = copy.Text("journey.count_one")
	} else {
		view.Result = copy.Text("journey.count_many", map[string]string{"count": copy.FormatNumber(strconv.Itoa(total), 0)})
	}
	if !filter.IsZero() {
		view.ClearLabel = copy.Text("history.clear")
		view.ClearHref = ListFilterHref(data.SelectedRef, productui.JourneyListFilter{})
	}
	if view.Narrowed && shown == 0 {
		view.EmptyTitle = copy.Text("journey.filter_empty_title")
		view.EmptyDetail = copy.Text("journey.filter_empty_detail")
	}
	return view
}

// listFilterFieldValues is the filter form's controlled values for one
// address, with the default sort and grouping spelled out so the selects
// show what the list is doing.
func listFilterFieldValues(filter productui.JourneyListFilter) map[string]string {
	filter = productui.NormalizeJourneyListFilter(filter)
	sortValue, groupValue := filter.Sort, filter.Group
	if sortValue == "" {
		sortValue = productui.JourneyListSortRecent
	}
	if groupValue == "" {
		groupValue = productui.JourneyListGroupPerson
	}
	return map[string]string{
		FieldFilterQuery: filter.Query, FieldFilterStatus: filter.Status,
		FieldFilterFrom: filter.From, FieldFilterTo: filter.To,
		FieldFilterSort: sortValue, FieldFilterGroup: groupValue,
	}
}

// syncListFilterValues makes the filter form show the address it is on.
// The form's controls are controlled inputs, so a value typed and never
// applied, or the state of a page the reader has gone Back from, would
// otherwise outlive the address that no longer says it.
func (a *App) syncListFilterValues(filter productui.JourneyListFilter) {
	values := listFilterFieldValues(filter)
	if a.listFilter.isPending() {
		// The reader is still typing a search that has not been applied;
		// the address it will produce replaces this one shortly.
		delete(values, FieldFilterQuery)
	}
	a.store.Update(func(page *journey.Page) {
		if page.Values == nil {
			page.Values = map[string]string{}
		}
		for id, value := range values {
			page.Values[id] = value
		}
	})
}

// ListFilterSearchDelay is how long the tracker waits after the last
// keystroke in its search box before applying it.
const ListFilterSearchDelay = 350 * time.Millisecond

// listFilterDebounce holds the pending search application. Its zero value
// is ready to use.
type listFilterDebounce struct {
	mu      sync.Mutex
	timer   *time.Timer
	pending bool
	// After schedules f; nil uses time.AfterFunc. Tests replace it.
	After func(time.Duration, func()) func() bool
}

func (d *listFilterDebounce) schedule(delay time.Duration, f func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.pending = true
	run := func() {
		d.mu.Lock()
		d.pending = false
		d.timer = nil
		d.mu.Unlock()
		f()
	}
	if d.After != nil {
		d.After(delay, run)
		return
	}
	d.timer = time.AfterFunc(delay, run)
}

func (d *listFilterDebounce) cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.pending = false
}

func (d *listFilterDebounce) isPending() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pending
}

// applyListFilterField makes the tracker's filter controls live: a status,
// order, grouping or date applies as soon as it changes, and the search
// applies after a pause in typing. Applying is a navigation to the filtered
// address, never a read, so Back steps through the reader's own choices. It
// reports whether fieldID was a filter control.
func (a *App) applyListFilterField(fieldID, value string) bool {
	if fieldID == journey.JourneyFilterPanelField {
		// Opening or closing the narrow-screen panel is presentation only.
		a.store.SetValue(fieldID, value)
		return true
	}
	switch fieldID {
	case FieldFilterQuery, FieldFilterStatus, FieldFilterFrom, FieldFilterTo, FieldFilterSort, FieldFilterGroup:
	default:
		return false
	}
	a.store.SetValue(fieldID, value)
	apply := func() {
		values := a.store.Values()
		a.mu.Lock()
		workerRef := a.route.WorkerRef
		a.mu.Unlock()
		a.Navigate(ListFilterHref(workerRef, ListFilterFromForm(map[string]string{
			productui.JourneyListQueryKey: values[FieldFilterQuery], productui.JourneyListStatusKey: values[FieldFilterStatus],
			productui.JourneyListFromKey: values[FieldFilterFrom], productui.JourneyListToKey: values[FieldFilterTo],
			productui.JourneyListSortKey: values[FieldFilterSort], productui.JourneyListGroupKey: values[FieldFilterGroup],
		})))
	}
	if fieldID == FieldFilterQuery {
		a.listFilter.schedule(ListFilterSearchDelay, apply)
		return true
	}
	a.listFilter.cancel()
	apply()
	return true
}
