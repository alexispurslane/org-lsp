package integration

import "testing"

// Given runs the setup function, calls t.Parallel(), then runs the test function.
// It handles LSPTestContext lifecycle automatically (including Shutdown).
func Given(name string, t *testing.T, setup func(*testing.T) *LSPTestContext, test func(*testing.T, *LSPTestContext)) bool {
	return t.Run("given "+name, func(t *testing.T) {
		tc := setup(t)
		defer tc.Shutdown()
		test(t, tc)
	})
}

// Then wraps t.Run with a "then " prefix for descriptive test output
func Then(name string, t *testing.T, fn func(*testing.T)) bool {
	return t.Run("then "+name, fn)
}
