package breadcrumb

import (
	"testing"

	"github.com/vibrantgio/prism/tokens"
)

// TestLabelColorRule asserts the Specific contract: in a breadcrumb of n
// items, the last segment renders in OnSurface (current location) and the
// preceding segments render in OnSurfaceVariant. Because the goldens use
// empty labels (font rasterisation is non-deterministic across GPUs), the
// per-segment foreground colour is not visually exercised — this pure-Go
// test guards the rule directly.
func TestLabelColorRule(t *testing.T) {
	for _, c := range []tokens.ColorTokens{tokens.DefaultLight, tokens.DefaultDark} {
		const n = 3
		for i := 0; i < n; i++ {
			got := labelColor(i, n, c)
			want := c.OnSurfaceVariant
			if i == n-1 {
				want = c.OnSurface
			}
			if got != want {
				t.Errorf("idx %d of %d (Surface=%v): got %v, want %v", i, n, c.Surface, got, want)
			}
		}
	}
}

// TestLabelColorSingleSegment confirms that with one item the lone
// segment is treated as the current location (OnSurface), matching the
// "last item" rule degenerate case.
func TestLabelColorSingleSegment(t *testing.T) {
	c := tokens.DefaultLight
	if got := labelColor(0, 1, c); got != c.OnSurface {
		t.Errorf("single segment: got %v, want OnSurface %v", got, c.OnSurface)
	}
}
