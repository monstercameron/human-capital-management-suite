package productclient

import (
	"context"
	"errors"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/invalidation"
)

// ErrRefreshNotApplied is retained for callers that want to classify an
// explicitly superseded operation. Refresh treats that condition as success
// because the newer projection is already visible.
var ErrRefreshNotApplied = errors.New("productclient: refresh superseded")

// ApplyView is the small composition seam between the authorized product
// client and the mounted UI. It receives only a server projection; an
// invalidation never supplies display data.
type ApplyView func(productui.View) error

// ProjectionRefresher serializes publication of projection reads while
// allowing the reads themselves to run concurrently. This matters when a
// reconnect catch-up and a live transition complete out of order: an older
// response must not replace a newer view. The baseline is replaced only after
// a successful publication, so LoadWithBaseline continues to preserve the
// already-authorized shell and refreshes only the destination's data regions.
type ProjectionRefresher struct {
	service Service
	session Session
	state   State
	apply   ApplyView

	mu        sync.Mutex
	publishMu sync.Mutex
	baseline  productui.View
	sequence  uint64
	loaded    bool
}

// NewProjectionRefresher creates a narrow invalidation-to-projection adapter.
// The initial baseline must come from the same authorized browser session.
func NewProjectionRefresher(service Service, session Session, state State, baseline productui.View, apply ApplyView) (*ProjectionRefresher, error) {
	if apply == nil {
		return nil, errors.New("productclient: refresh apply function is nil")
	}
	return &ProjectionRefresher{service: service, session: session, state: state, baseline: baseline, apply: apply}, nil
}

// Refresh performs one authorized, affected-region projection read. The
// sequence is a freshness hint only; authorization remains in Service. A
// duplicate or superseded sequence is ignored without a render.
func (r *ProjectionRefresher) Refresh(ctx context.Context, hint invalidation.Refresh) error {
	if r == nil {
		return errors.New("productclient: nil refresher")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if hint.SourceSequence <= r.sequence {
		r.mu.Unlock()
		return nil
	}
	baseline := r.baseline
	r.mu.Unlock()

	view, err := LoadWithBaseline(ctx, r.service, r.session, r.state, baseline)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Publication is serialized separately from the small state lock. The
	// apply callback is allowed to inspect Snapshot or trigger UI bookkeeping.
	r.publishMu.Lock()
	defer r.publishMu.Unlock()
	r.mu.Lock()
	if hint.SourceSequence <= r.sequence {
		r.mu.Unlock()
		// A newer projection is already visible. This is successful catch-up,
		// not a failed RPC, and must not make the subscription reconnect.
		return nil
	}
	if err := r.apply(view); err != nil {
		r.mu.Unlock()
		return err
	}
	r.baseline = view
	r.sequence = hint.SourceSequence
	r.loaded = true
	r.mu.Unlock()
	return nil
}

// Snapshot returns the last published projection and source sequence. The
// returned view is a value copy; its slices remain owned by the product UI.
func (r *ProjectionRefresher) Snapshot() (productui.View, uint64, bool) {
	if r == nil {
		return productui.View{}, 0, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.baseline, r.sequence, r.loaded
}
