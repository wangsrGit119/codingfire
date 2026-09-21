//go:build !windows

package main

// singleInstance is a no-op off Windows. The shipped app is Windows-only; this
// exists so the headless modes still build elsewhere.
func singleInstance(name string) (func(), bool) { return func() {}, false }
