// Package projectworkflow validates and seals project-owned task workflow
// configuration. It has no persistence, authorization, or project-domain
// dependencies so callers can use it from command and application boundaries.
package projectworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MaxTaskTypes    = 25
	MaxStatuses     = 50
	MaxTransitions  = 500
	MaxActiveFields = 25
	MaxEnumOptions  = 100
	MaxColumns      = 50
	MaxIDLength     = 64
	MaxLabelLength  = 120
)

var (
	ErrInvalidConfig        = errors.New("projectworkflow: invalid config")
	ErrInvalidVersion       = errors.New("projectworkflow: invalid version")
	ErrTransitionRejected   = errors.New("projectworkflow: transition rejected")
	ErrTaskCreationRejected = errors.New("projectworkflow: task creation rejected")
)

type StatusCategory string

const (
	CategoryNotStarted StatusCategory = "NOT_STARTED"
	CategoryActive     StatusCategory = "ACTIVE"
	CategoryBlocked    StatusCategory = "BLOCKED"
	CategoryDone       StatusCategory = "DONE"
	CategoryCancelled  StatusCategory = "CANCELLED"
)

type FieldType string

const (
	FieldText    FieldType = "TEXT"
	FieldNumber  FieldType = "NUMBER"
	FieldDate    FieldType = "DATE"
	FieldEnum    FieldType = "ENUM"
	FieldPerson  FieldType = "PERSON"
	FieldLink    FieldType = "LINK"
	FieldBoolean FieldType = "BOOLEAN"
)

type Status struct {
	ID                   string         `json:"id"`
	Name                 string         `json:"name"`
	Category             StatusCategory `json:"category"`
	AllowedNextStatusIDs []string       `json:"allowed_next_status_ids,omitempty"`
	RequiredFieldIDs     []string       `json:"required_field_ids,omitempty"`
	Retired              bool           `json:"retired,omitempty"`
}

type TaskType struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	FieldIDs       []string `json:"field_ids,omitempty"`
	InitialStatus  string   `json:"initial_status"`
	RequiredFields []string `json:"required_fields,omitempty"`
}

type Transition struct {
	From           string   `json:"from"`
	To             string   `json:"to"`
	TaskTypeID     string   `json:"task_type_id,omitempty"`
	RequiredFields []string `json:"required_fields,omitempty"`
}

type FieldValidation struct {
	MinLength *int     `json:"min_length,omitempty"`
	MaxLength *int     `json:"max_length,omitempty"`
	MinNumber *float64 `json:"min_number,omitempty"`
	MaxNumber *float64 `json:"max_number,omitempty"`
	Options   []string `json:"options,omitempty"`
}

type Field struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Type           FieldType       `json:"type"`
	Required       bool            `json:"required,omitempty"`
	Validation     FieldValidation `json:"validation"`
	Classification string          `json:"classification"`
	Default        json.RawMessage `json:"default,omitempty"`
	Indexed        bool            `json:"indexed,omitempty"`
	Searchable     bool            `json:"searchable,omitempty"`
	Retired        bool            `json:"retired,omitempty"`
}

type Column struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	StatusIDs []string `json:"status_ids"`
}

type Config struct {
	TaskTypes   []TaskType   `json:"task_types"`
	Statuses    []Status     `json:"statuses"`
	Transitions []Transition `json:"transitions"`
	Fields      []Field      `json:"fields"`
	Columns     []Column     `json:"columns"`
}

type ValidationError struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string { return e.Code + " at " + e.Path + ": " + e.Message }

type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	return fmt.Sprintf("projectworkflow: %d validation error(s): %s", len(e), e[0].Error())
}

func (e ValidationErrors) Is(target error) bool { return target == ErrInvalidConfig }

type PublishedVersion struct {
	version uint64
	digest  string
	config  Config
}

// TransitionInput contains the optimistic config pin and task state needed to
// decide a move. Edits override current values for validation only.
type TransitionInput struct {
	ExpectedConfigVersion uint64                     `json:"expected_config_version"`
	TaskTypeID            string                     `json:"task_type_id"`
	FromStatusID          string                     `json:"from_status_id"`
	ToStatusID            string                     `json:"to_status_id"`
	CurrentFields         map[string]json.RawMessage `json:"current_fields,omitempty"`
	FieldEdits            map[string]json.RawMessage `json:"field_edits,omitempty"`
}

type TransitionErrors []ValidationError

func (e TransitionErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	return fmt.Sprintf("projectworkflow: transition rejected: %s", e[0].Error())
}
func (e TransitionErrors) Is(target error) bool { return target == ErrTransitionRejected }

type TaskCreationErrors []ValidationError

func (e TaskCreationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	return fmt.Sprintf("projectworkflow: task creation rejected: %s", e[0].Error())
}
func (e TaskCreationErrors) Is(target error) bool { return target == ErrTaskCreationRejected }

var stableID = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

// Validate checks bounded workflow configuration and returns deterministic,
// machine-readable errors. IDs are opaque stable identifiers and labels may
// change independently.
func Validate(c Config) ValidationErrors {
	var errs ValidationErrors
	add := func(code, path, message string) {
		errs = append(errs, ValidationError{Code: code, Path: path, Message: message})
	}
	if len(c.TaskTypes) == 0 || len(c.TaskTypes) > MaxTaskTypes {
		add("LIMIT_TASK_TYPES", "task_types", "must contain between 1 and 25 task types")
	}
	if len(c.Statuses) < 2 || len(c.Statuses) > MaxStatuses {
		add("LIMIT_STATUSES", "statuses", "must contain between 2 and 50 statuses")
	}
	if len(c.Transitions) > MaxTransitions {
		add("LIMIT_TRANSITIONS", "transitions", "must contain at most 500 transitions")
	}
	if len(c.Columns) == 0 || len(c.Columns) > MaxColumns {
		add("LIMIT_COLUMNS", "columns", "must contain between 1 and 50 columns")
	}

	statuses := map[string]Status{}
	for i, s := range c.Statuses {
		p := fmt.Sprintf("statuses[%d]", i)
		checkID := func(id, path string) {
			if !stableID.MatchString(id) {
				add("INVALID_ID", path, "must be a stable identifier of 1 to 64 ASCII letters, digits, underscore, or hyphen")
			}
		}
		checkID(s.ID, p+".id")
		if _, ok := statuses[s.ID]; ok {
			add("DUPLICATE_STATUS_ID", p+".id", "status ID is already defined")
		}
		statuses[s.ID] = s
		checkLabel(s.Name, p+".name", add)
		if !validCategory(s.Category) {
			add("INVALID_STATUS_CATEGORY", p+".category", "category is not supported")
		}
		seenNext := map[string]bool{}
		for j, nextID := range s.AllowedNextStatusIDs {
			if seenNext[nextID] {
				add("DUPLICATE_ALLOWED_TRANSITION", fmt.Sprintf("%s.allowed_next_status_ids[%d]", p, j), "target status is already allowed")
			}
			seenNext[nextID] = true
			if nextID == s.ID {
				add("SELF_TRANSITION", fmt.Sprintf("%s.allowed_next_status_ids[%d]", p, j), "a status cannot transition to itself")
			}
		}
	}
	fields := map[string]Field{}
	activeFields := 0
	for i, f := range c.Fields {
		p := fmt.Sprintf("fields[%d]", i)
		if !stableID.MatchString(f.ID) {
			add("INVALID_ID", p+".id", "must be a stable identifier of 1 to 64 ASCII letters, digits, underscore, or hyphen")
		}
		if _, ok := fields[f.ID]; ok {
			add("DUPLICATE_FIELD_ID", p+".id", "field ID is already defined")
		}
		fields[f.ID] = f
		checkLabel(f.Name, p+".name", add)
		if !f.Retired {
			activeFields++
		}
		if !validFieldType(f.Type) {
			add("INVALID_FIELD_TYPE", p+".type", "field type is not supported")
		}
		if f.Classification == "" {
			add("MISSING_CLASSIFICATION", p+".classification", "classification is required")
		}
		validateField(f, p, add)
	}
	if activeFields > MaxActiveFields {
		add("LIMIT_ACTIVE_FIELDS", "fields", "must contain at most 25 active fields")
	}

	types := map[string]TaskType{}
	for i, tt := range c.TaskTypes {
		p := fmt.Sprintf("task_types[%d]", i)
		if !stableID.MatchString(tt.ID) {
			add("INVALID_ID", p+".id", "must be a stable identifier of 1 to 64 ASCII letters, digits, underscore, or hyphen")
		}
		if _, ok := types[tt.ID]; ok {
			add("DUPLICATE_TASK_TYPE_ID", p+".id", "task type ID is already defined")
		}
		types[tt.ID] = tt
		checkLabel(tt.Name, p+".name", add)
		initial, ok := statuses[tt.InitialStatus]
		if !ok || initial.Retired {
			add("INVALID_INITIAL_STATUS", p+".initial_status", "must name an active status")
		}
		validateRequiredFields(tt.RequiredFields, fields, p+".required_fields", add)
		allowedFields := validateFieldIDs(tt.FieldIDs, fields, p+".field_ids", add)
		for j, id := range tt.RequiredFields {
			if !allowedFields[id] {
				add("REQUIRED_FIELD_NOT_ALLOWED", fmt.Sprintf("%s.required_fields[%d]", p, j), "required field must be included in this task type's field IDs")
			}
		}
	}
	for i, s := range c.Statuses {
		path := fmt.Sprintf("statuses[%d]", i)
		for j, nextID := range s.AllowedNextStatusIDs {
			target, ok := statuses[nextID]
			if !ok || target.Retired {
				add("INVALID_ALLOWED_TRANSITION", fmt.Sprintf("%s.allowed_next_status_ids[%d]", path, j), "target must name an active status")
			}
		}
		validateRequiredFields(s.RequiredFieldIDs, fields, path+".required_field_ids", add)
	}
	transitionKeys := map[string]bool{}
	for i, tr := range c.Transitions {
		p := fmt.Sprintf("transitions[%d]", i)
		from, fromOK := statuses[tr.From]
		to, toOK := statuses[tr.To]
		if !fromOK || from.Retired {
			add("INVALID_TRANSITION_SOURCE", p+".from", "must name an active status")
		}
		if !toOK || to.Retired {
			add("INVALID_TRANSITION_TARGET", p+".to", "must name an active status")
		}
		if tr.From == tr.To {
			add("SELF_TRANSITION", p, "a transition must change status")
		}
		if fromOK && !contains(from.AllowedNextStatusIDs, tr.To) {
			add("TRANSITION_NOT_ALLOWED_BY_STATUS", p, "transition must be listed in the source status allowed-next IDs")
		}
		if tr.TaskTypeID != "" {
			if _, ok := types[tr.TaskTypeID]; !ok {
				add("INVALID_TRANSITION_TYPE", p+".task_type_id", "must name a configured task type")
			}
		}
		key := tr.TaskTypeID + "\x00" + tr.From + "\x00" + tr.To
		if transitionKeys[key] {
			add("DUPLICATE_TRANSITION", p, "transition is already configured")
		}
		transitionKeys[key] = true
		validateRequiredFields(tr.RequiredFields, fields, p+".required_fields", add)
		if tr.TaskTypeID != "" {
			if taskType, ok := types[tr.TaskTypeID]; ok {
				for j, id := range tr.RequiredFields {
					if !contains(taskType.FieldIDs, id) {
						add("REQUIRED_FIELD_NOT_ALLOWED", fmt.Sprintf("%s.required_fields[%d]", p, j), "required field must be enabled for this task type")
					}
				}
			}
		} else {
			for _, taskType := range c.TaskTypes {
				for j, id := range tr.RequiredFields {
					if !contains(taskType.FieldIDs, id) {
						add("REQUIRED_FIELD_NOT_ALLOWED", fmt.Sprintf("%s.required_fields[%d]", p, j), "globally required transition field must be enabled for every task type")
					}
				}
			}
		}
	}
	// Each type must have a path from its initial status to every active status.
	for i, tt := range c.TaskTypes {
		if _, ok := statuses[tt.InitialStatus]; !ok {
			continue
		}
		reachable := reachableStatuses(tt.InitialStatus, c.Statuses, c.Transitions, tt.ID)
		for _, s := range c.Statuses {
			if !s.Retired && !reachable[s.ID] {
				add("UNREACHABLE_STATUS", fmt.Sprintf("task_types[%d]", i), "status "+s.ID+" cannot be reached from this type's initial status")
			}
			if reachable[s.ID] {
				for j, id := range s.RequiredFieldIDs {
					if !contains(tt.FieldIDs, id) {
						add("REQUIRED_FIELD_NOT_ALLOWED", fmt.Sprintf("statuses.%s.required_field_ids[%d]", s.ID, j), "required field must be enabled for every task type that can reach this status")
					}
				}
			}
		}
	}

	columnIDs, mapped := map[string]bool{}, map[string]bool{}
	for i, col := range c.Columns {
		p := fmt.Sprintf("columns[%d]", i)
		if !stableID.MatchString(col.ID) {
			add("INVALID_ID", p+".id", "must be a stable identifier of 1 to 64 ASCII letters, digits, underscore, or hyphen")
		}
		if columnIDs[col.ID] {
			add("DUPLICATE_COLUMN_ID", p+".id", "column ID is already defined")
		}
		columnIDs[col.ID] = true
		checkLabel(col.Name, p+".name", add)
		if len(col.StatusIDs) == 0 {
			add("EMPTY_COLUMN_MAPPING", p+".status_ids", "column must map at least one status")
		}
		for j, id := range col.StatusIDs {
			if _, ok := statuses[id]; !ok || statuses[id].Retired {
				add("INVALID_COLUMN_STATUS", fmt.Sprintf("%s.status_ids[%d]", p, j), "must name an active status")
			}
			if mapped[id] {
				add("DUPLICATE_COLUMN_STATUS", fmt.Sprintf("%s.status_ids[%d]", p, j), "status is already mapped to a column")
			}
			mapped[id] = true
		}
	}
	for _, s := range c.Statuses {
		if !s.Retired && !mapped[s.ID] {
			add("UNMAPPED_STATUS", "columns", "active status "+s.ID+" has no board column")
		}
	}
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].Path == errs[j].Path {
			if errs[i].Code == errs[j].Code {
				return errs[i].Message < errs[j].Message
			}
			return errs[i].Code < errs[j].Code
		}
		return errs[i].Path < errs[j].Path
	})
	return errs
}

// Publish validates and takes a deep snapshot. A published value exposes
// content and digest; callers receive a fresh snapshot through Snapshot.
func Publish(c Config, version uint64) (PublishedVersion, error) {
	if version == 0 {
		return PublishedVersion{}, ErrInvalidVersion
	}
	if errs := Validate(c); len(errs) != 0 {
		return PublishedVersion{}, errs
	}
	c = cloneConfig(c)
	b, err := json.Marshal(c)
	if err != nil {
		return PublishedVersion{}, fmt.Errorf("projectworkflow: encode config: %w", err)
	}
	sum := sha256.Sum256(b)
	return PublishedVersion{version: version, digest: hex.EncodeToString(sum[:]), config: c}, nil
}

// Snapshot returns an independent copy so callers cannot mutate the sealed value.
func (p PublishedVersion) Version() uint64  { return p.version }
func (p PublishedVersion) Digest() string   { return p.digest }
func (p PublishedVersion) Snapshot() Config { return cloneConfig(p.config) }

// ValidateTransition checks a single move against the exact published version
// and validates the post-edit field set before allowing required-field checks.
func ValidateTransition(p PublishedVersion, in TransitionInput) TransitionErrors {
	var errs TransitionErrors
	add := func(code, path, message string) {
		errs = append(errs, ValidationError{Code: code, Path: path, Message: message})
	}
	c := p.config
	if p.version == 0 || in.ExpectedConfigVersion != p.version {
		add("CONFIG_VERSION_CONFLICT", "expected_config_version", "expected configuration version does not match the published version")
	}
	typeFound := false
	var taskType TaskType
	for _, tt := range c.TaskTypes {
		if tt.ID == in.TaskTypeID {
			typeFound = true
			taskType = tt
			break
		}
	}
	if !typeFound {
		add("UNKNOWN_TASK_TYPE", "task_type_id", "task type is not in this published configuration")
	}
	statuses := map[string]Status{}
	for _, s := range c.Statuses {
		statuses[s.ID] = s
	}
	from, fromOK := statuses[in.FromStatusID]
	to, toOK := statuses[in.ToStatusID]
	if !fromOK || from.Retired {
		add("UNKNOWN_SOURCE_STATUS", "from_status_id", "source status is not active in this published configuration")
	}
	if !toOK || to.Retired {
		add("UNKNOWN_TARGET_STATUS", "to_status_id", "target status is not active in this published configuration")
	}
	allowed := fromOK && contains(from.AllowedNextStatusIDs, in.ToStatusID)
	hasEdgePolicy := false
	for _, tr := range c.Transitions {
		if tr.From == in.FromStatusID && tr.To == in.ToStatusID {
			hasEdgePolicy = true
			if tr.TaskTypeID == "" || tr.TaskTypeID == in.TaskTypeID {
				allowed = true
			}
		}
	}
	if allowed && hasEdgePolicy {
		policyFound := false
		for _, tr := range c.Transitions {
			if tr.From == in.FromStatusID && tr.To == in.ToStatusID && (tr.TaskTypeID == "" || tr.TaskTypeID == in.TaskTypeID) {
				policyFound = true
				break
			}
		}
		allowed = policyFound
	}
	if !allowed {
		add("TRANSITION_NOT_ALLOWED", "to_status_id", "the published workflow does not allow this transition for the task type")
	}

	fieldDefs := map[string]Field{}
	for _, f := range c.Fields {
		fieldDefs[f.ID] = f
	}
	values := cloneRawMap(in.CurrentFields)
	for id, value := range in.FieldEdits {
		values[id] = append(json.RawMessage(nil), value...)
	}
	for id, value := range values {
		f, ok := fieldDefs[id]
		if !ok || f.Retired {
			add("UNKNOWN_FIELD", "fields."+id, "field is not active in this published configuration")
			continue
		}
		if typeFound && !contains(taskType.FieldIDs, id) {
			add("FIELD_NOT_ALLOWED_FOR_TASK_TYPE", "fields."+id, "field is not enabled for this task type")
			continue
		}
		if message := validateValue(f, value); message != "" {
			add("INVALID_FIELD_VALUE", "fields."+id, message)
		}
	}
	required := map[string]bool{}
	if typeFound {
		for _, id := range taskType.RequiredFields {
			required[id] = true
		}
		for _, id := range taskType.FieldIDs {
			if field, ok := fieldDefs[id]; ok && field.Required {
				required[id] = true
			}
		}
	}
	if toOK {
		for _, id := range to.RequiredFieldIDs {
			required[id] = true
		}
	}
	for _, tr := range c.Transitions {
		if tr.From == in.FromStatusID && tr.To == in.ToStatusID && (tr.TaskTypeID == "" || tr.TaskTypeID == in.TaskTypeID) {
			for _, id := range tr.RequiredFields {
				required[id] = true
			}
		}
	}
	for id := range required {
		value, ok := values[id]
		if !ok || !hasValue(value) {
			add("REQUIRED_FIELD_MISSING", "fields."+id, "required field must have a value before this transition")
		}
	}
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].Path == errs[j].Path {
			return errs[i].Code < errs[j].Code
		}
		return errs[i].Path < errs[j].Path
	})
	return errs
}

// ValidateTaskCreation validates the initial state against an active workflow
// version before a caller persists a new task.
func ValidateTaskCreation(config Config, expectedVersion, actualVersion uint64, typeID, initialStatusID string, values map[string]json.RawMessage) error {
	var errs TaskCreationErrors
	add := func(code, path, message string) {
		errs = append(errs, ValidationError{Code: code, Path: path, Message: message})
	}
	if expectedVersion == 0 || actualVersion == 0 || expectedVersion != actualVersion {
		add("CONFIG_VERSION_CONFLICT", "expected_config_version", "expected configuration version does not match the active version")
	}
	types := taskTypeIndex(config)
	taskType, typeOK := types[typeID]
	if !typeOK {
		add("UNKNOWN_TASK_TYPE", "task_type_id", "task type is not configured")
	}
	statuses := statusIndex(config)
	status, statusOK := statuses[initialStatusID]
	if !statusOK || status.Retired {
		add("UNKNOWN_INITIAL_STATUS", "initial_status_id", "initial status is not active")
	}
	if typeOK && taskType.InitialStatus != initialStatusID {
		add("INVALID_INITIAL_STATUS", "initial_status_id", "status does not match the task type initial status")
	}
	fields := fieldIndex(config)
	for id, value := range values {
		field, ok := fields[id]
		if !ok || field.Retired {
			add("UNKNOWN_FIELD", "fields."+id, "field is not active in the configuration")
			continue
		}
		if typeOK && !contains(taskType.FieldIDs, id) {
			add("FIELD_NOT_ALLOWED_FOR_TASK_TYPE", "fields."+id, "field is not enabled for this task type")
			continue
		}
		if message := validateValue(field, value); message != "" {
			add("INVALID_FIELD_VALUE", "fields."+id, message)
		}
	}
	required := map[string]bool{}
	if typeOK {
		for _, id := range taskType.RequiredFields {
			required[id] = true
		}
		for _, id := range taskType.FieldIDs {
			if field, ok := fields[id]; ok && field.Required {
				required[id] = true
			}
		}
	}
	if statusOK {
		for _, id := range status.RequiredFieldIDs {
			required[id] = true
		}
	}
	for id := range required {
		if value, present := values[id]; !present || !hasValue(value) {
			add("REQUIRED_FIELD_MISSING", "fields."+id, "required field must have a value when creating this task")
		}
	}
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].Path == errs[j].Path {
			return errs[i].Code < errs[j].Code
		}
		return errs[i].Path < errs[j].Path
	})
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func cloneConfig(c Config) Config {
	c.TaskTypes = append([]TaskType(nil), c.TaskTypes...)
	for i := range c.TaskTypes {
		c.TaskTypes[i].FieldIDs = append([]string(nil), c.TaskTypes[i].FieldIDs...)
		c.TaskTypes[i].RequiredFields = append([]string(nil), c.TaskTypes[i].RequiredFields...)
	}
	c.Statuses = append([]Status(nil), c.Statuses...)
	for i := range c.Statuses {
		c.Statuses[i].AllowedNextStatusIDs = append([]string(nil), c.Statuses[i].AllowedNextStatusIDs...)
		c.Statuses[i].RequiredFieldIDs = append([]string(nil), c.Statuses[i].RequiredFieldIDs...)
	}
	c.Transitions = append([]Transition(nil), c.Transitions...)
	for i := range c.Transitions {
		c.Transitions[i].RequiredFields = append([]string(nil), c.Transitions[i].RequiredFields...)
	}
	c.Fields = append([]Field(nil), c.Fields...)
	for i := range c.Fields {
		c.Fields[i].Default = append(json.RawMessage(nil), c.Fields[i].Default...)
		c.Fields[i].Validation.Options = append([]string(nil), c.Fields[i].Validation.Options...)
		c.Fields[i].Validation.MinLength = cloneInt(c.Fields[i].Validation.MinLength)
		c.Fields[i].Validation.MaxLength = cloneInt(c.Fields[i].Validation.MaxLength)
		c.Fields[i].Validation.MinNumber = cloneFloat(c.Fields[i].Validation.MinNumber)
		c.Fields[i].Validation.MaxNumber = cloneFloat(c.Fields[i].Validation.MaxNumber)
	}
	c.Columns = append([]Column(nil), c.Columns...)
	for i := range c.Columns {
		c.Columns[i].StatusIDs = append([]string(nil), c.Columns[i].StatusIDs...)
	}
	return c
}
func cloneInt(v *int) *int {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}
func cloneFloat(v *float64) *float64 {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}
func checkLabel(s, path string, add func(string, string, string)) {
	if strings.TrimSpace(s) == "" || len([]rune(s)) > MaxLabelLength {
		add("INVALID_LABEL", path, "must be non-empty and at most 120 characters")
	}
}
func validCategory(c StatusCategory) bool {
	return c == CategoryNotStarted || c == CategoryActive || c == CategoryBlocked || c == CategoryDone || c == CategoryCancelled
}
func validFieldType(t FieldType) bool {
	return t == FieldText || t == FieldNumber || t == FieldDate || t == FieldEnum || t == FieldPerson || t == FieldLink || t == FieldBoolean
}
func validateRequiredFields(ids []string, fields map[string]Field, path string, add func(string, string, string)) {
	seen := map[string]bool{}
	for i, id := range ids {
		p := fmt.Sprintf("%s[%d]", path, i)
		f, ok := fields[id]
		if !ok || f.Retired {
			add("INVALID_REQUIRED_FIELD", p, "must name an active field")
		}
		if seen[id] {
			add("DUPLICATE_REQUIRED_FIELD", p, "field is already required")
		}
		seen[id] = true
	}
}

func validateFieldIDs(ids []string, fields map[string]Field, path string, add func(string, string, string)) map[string]bool {
	allowed := make(map[string]bool, len(ids))
	for i, id := range ids {
		p := fmt.Sprintf("%s[%d]", path, i)
		field, ok := fields[id]
		if !ok || field.Retired {
			add("INVALID_TASK_TYPE_FIELD", p, "must name an active field")
		}
		if allowed[id] {
			add("DUPLICATE_TASK_TYPE_FIELD", p, "field ID is already enabled for this task type")
		}
		allowed[id] = true
	}
	return allowed
}

func contains(ids []string, wanted string) bool {
	for _, id := range ids {
		if id == wanted {
			return true
		}
	}
	return false
}
func validateField(f Field, p string, add func(string, string, string)) {
	v := f.Validation
	if (v.MinLength != nil || v.MaxLength != nil) && f.Type != FieldText {
		add("INVALID_FIELD_VALIDATION", p+".validation", "length bounds are supported only for TEXT fields")
	}
	if (v.MinNumber != nil || v.MaxNumber != nil) && f.Type != FieldNumber {
		add("INVALID_FIELD_VALIDATION", p+".validation", "numeric bounds are supported only for NUMBER fields")
	}
	if v.MinLength != nil && *v.MinLength < 0 {
		add("INVALID_FIELD_VALIDATION", p+".validation.min_length", "must be non-negative")
	}
	if v.MaxLength != nil && (*v.MaxLength < 0 || (v.MinLength != nil && *v.MaxLength < *v.MinLength)) {
		add("INVALID_FIELD_VALIDATION", p+".validation.max_length", "must be non-negative and no smaller than min_length")
	}
	if v.MinNumber != nil && v.MaxNumber != nil && *v.MinNumber > *v.MaxNumber {
		add("INVALID_FIELD_VALIDATION", p+".validation", "min_number must not exceed max_number")
	}
	if f.Type == FieldEnum {
		if len(v.Options) == 0 || len(v.Options) > MaxEnumOptions {
			add("INVALID_ENUM_OPTIONS", p+".validation.options", "enum must have between 1 and 100 options")
		}
		seen := map[string]bool{}
		for i, option := range v.Options {
			if strings.TrimSpace(option) == "" || len([]rune(option)) > MaxLabelLength {
				add("INVALID_ENUM_OPTION", fmt.Sprintf("%s.validation.options[%d]", p, i), "option must be non-empty and at most 120 characters")
			}
			if seen[option] {
				add("DUPLICATE_ENUM_OPTION", fmt.Sprintf("%s.validation.options[%d]", p, i), "option is already defined")
			}
			seen[option] = true
		}
	} else if len(v.Options) > 0 {
		add("UNEXPECTED_ENUM_OPTIONS", p+".validation.options", "options are supported only for ENUM fields")
	}
	if len(f.Default) > 0 && !json.Valid(f.Default) {
		add("INVALID_DEFAULT", p+".default", "default must be valid JSON")
	} else if len(f.Default) > 0 && validateValue(f, f.Default) != "" {
		add("INVALID_DEFAULT", p+".default", "default does not satisfy the field type and validation")
	}
}

func validateValue(f Field, raw json.RawMessage) string {
	if !json.Valid(raw) {
		return "must be valid JSON"
	}
	var value any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return "must be valid JSON"
	}
	switch f.Type {
	case FieldText:
		s, ok := value.(string)
		if !ok {
			return "must be text"
		}
		n := len([]rune(s))
		if f.Validation.MinLength != nil && n < *f.Validation.MinLength {
			return "is shorter than the minimum length"
		}
		if f.Validation.MaxLength != nil && n > *f.Validation.MaxLength {
			return "is longer than the maximum length"
		}
	case FieldNumber:
		n, ok := value.(json.Number)
		if !ok {
			return "must be a number"
		}
		parsed, err := strconv.ParseFloat(string(n), 64)
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return "must be a finite number"
		}
		if f.Validation.MinNumber != nil && parsed < *f.Validation.MinNumber {
			return "is below the minimum"
		}
		if f.Validation.MaxNumber != nil && parsed > *f.Validation.MaxNumber {
			return "is above the maximum"
		}
	case FieldDate:
		s, ok := value.(string)
		if !ok {
			return "must be an ISO calendar date"
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return "must be an ISO calendar date"
		}
	case FieldEnum:
		s, ok := value.(string)
		if !ok {
			return "must be an enum option"
		}
		for _, option := range f.Validation.Options {
			if s == option {
				return ""
			}
		}
		return "must match a configured enum option"
	case FieldPerson, FieldLink:
		s, ok := value.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return "must be a non-empty reference"
		}
	case FieldBoolean:
		if _, ok := value.(bool); !ok {
			return "must be true or false"
		}
	default:
		return "field type is unsupported"
	}
	return ""
}

func hasValue(raw json.RawMessage) bool {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s) != ""
	}
	return true
}

func cloneRawMap(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(in))
	for id, value := range in {
		out[id] = append(json.RawMessage(nil), value...)
	}
	return out
}
func reachableStatuses(start string, statuses []Status, transitions []Transition, typeID string) map[string]bool {
	adj := map[string][]string{}
	for _, status := range statuses {
		for _, target := range status.AllowedNextStatusIDs {
			hasPolicy, matchesType := false, false
			for _, tr := range transitions {
				if tr.From == status.ID && tr.To == target {
					hasPolicy = true
					if tr.TaskTypeID == "" || tr.TaskTypeID == typeID {
						matchesType = true
					}
				}
			}
			if !hasPolicy || matchesType {
				adj[status.ID] = append(adj[status.ID], target)
			}
		}
	}
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, to := range adj[n] {
			if !seen[to] {
				seen[to] = true
				queue = append(queue, to)
			}
		}
	}
	return seen
}
