package modal

import (
	"sync"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/prism/coordination"
)

// StackSnapshot is a snapshot of the open-modal stack, bottom-to-top.
// Consumers receive a new snapshot every time a modal opens or closes.
type StackSnapshot struct {
	Open []int64
}

var (
	stackMu      sync.Mutex
	stackOpen    []int64
	stackNextID  int64
	stackPublish rx.Observer[StackSnapshot]
	// Stack is the cross-modal coordination Observable. Downstream patterns
	// (popover, tooltip, drag overlay) subscribe to learn when a modal is
	// in front of them; modal itself reads the stack synchronously via
	// isTop at frame time and does not subscribe.
	Stack rx.Observable[StackSnapshot]
)

func init() {
	stackPublish, Stack = coordination.Subject[StackSnapshot](coordination.BufCapSignal)
}

// allocStackID returns a fresh modal id. Each Modal subscription allocates
// one in its rx.Defer scope.
func allocStackID() int64 {
	stackMu.Lock()
	defer stackMu.Unlock()
	stackNextID++
	return stackNextID
}

// stackPush adds id to the top of the stack and publishes a snapshot.
// Returns early without publishing if id is already on the stack.
func stackPush(id int64) {
	stackMu.Lock()
	for _, x := range stackOpen {
		if x == id {
			stackMu.Unlock()
			return
		}
	}
	stackOpen = append(stackOpen, id)
	snap := StackSnapshot{Open: append([]int64(nil), stackOpen...)}
	stackMu.Unlock()
	stackPublish.Next(snap)
}

// stackPop removes id from the stack and publishes a snapshot. Returns
// early without publishing if id is not on the stack.
func stackPop(id int64) {
	stackMu.Lock()
	idx := -1
	for i, x := range stackOpen {
		if x == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		stackMu.Unlock()
		return
	}
	stackOpen = append(stackOpen[:idx], stackOpen[idx+1:]...)
	snap := StackSnapshot{Open: append([]int64(nil), stackOpen...)}
	stackMu.Unlock()
	stackPublish.Next(snap)
}

// isTop reports whether id is the topmost open modal. Only the topmost
// modal processes keyboard and pointer input; modals beneath remain
// painted but inert.
func isTop(id int64) bool {
	stackMu.Lock()
	defer stackMu.Unlock()
	return len(stackOpen) > 0 && stackOpen[len(stackOpen)-1] == id
}

