package tooltip

import (
	"sync"
	"sync/atomic"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/prism/coordination"
)

// ArbitrationSnapshot reports the id of the currently-visible tooltip. A
// Top of zero means no tooltip is shown. Consumers receive a snapshot
// whenever a tooltip becomes visible, hides, or another tooltip overtakes
// it.
type ArbitrationSnapshot struct{ Top int64 }

var (
	arbMu      sync.Mutex
	arbTop     int64
	arbNextID  atomic.Int64
	arbPublish rx.Observer[ArbitrationSnapshot]
	// Arbitration is the cross-tooltip coordination Observable. Only one
	// tooltip may be visible at a time; showing a new tooltip takes top
	// and the previous tooltip observes the change at frame time and
	// hides. Each Tooltip subscription queries currentTop synchronously
	// via isTop and does not subscribe to the observable itself.
	Arbitration rx.Observable[ArbitrationSnapshot]
)

func init() {
	arbPublish, Arbitration = coordination.Subject[ArbitrationSnapshot](coordination.BufCapSignal)
}

// allocID returns a fresh tooltip id. Each Tooltip subscription allocates
// one in its rx.Defer scope.
func allocID() int64 { return arbNextID.Add(1) }

// setTop claims arbitration top for id and publishes a snapshot.
func setTop(id int64) {
	arbMu.Lock()
	arbTop = id
	snap := ArbitrationSnapshot{Top: id}
	arbMu.Unlock()
	arbPublish.Next(snap)
}

// clearTop releases arbitration top if id currently holds it. Returns
// without publishing if id does not hold top (a later tooltip already
// overtook us).
func clearTop(id int64) {
	arbMu.Lock()
	if arbTop != id {
		arbMu.Unlock()
		return
	}
	arbTop = 0
	snap := ArbitrationSnapshot{Top: 0}
	arbMu.Unlock()
	arbPublish.Next(snap)
}

// isTop reports whether id currently holds arbitration top.
func isTop(id int64) bool {
	arbMu.Lock()
	defer arbMu.Unlock()
	return arbTop == id
}
