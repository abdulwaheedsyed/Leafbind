//go:build !windows && !darwin

package window

func run(Options) error { return ErrUnavailable }

func closeWindow() {}
