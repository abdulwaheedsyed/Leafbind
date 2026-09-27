//go:build windows

package window

import (
	"runtime"
	"sync"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
)

var (
	mu   sync.Mutex
	open webview2.WebView
)

func run(o Options) error {
	// Without the WebView2 runtime there is nothing to show; checking first
	// avoids a window that appears and vanishes.
	if _, err := webviewloader.GetInstalledVersion(); err != nil {
		return ErrUnavailable
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  o.DataDir,
		WindowOptions: webview2.WindowOptions{
			Title:  o.Title,
			Width:  uint(o.Width),
			Height: uint(o.Height),
			IconId: 1, // the icon group in the executable's resources
			Center: true,
		},
	})
	if w == nil {
		return ErrUnavailable
	}
	if o.MinW > 0 {
		w.SetSize(o.MinW, o.MinH, webview2.HintMin)
	}
	w.Navigate(o.URL)
	mu.Lock()
	open = w
	mu.Unlock()
	w.Run()
	mu.Lock()
	open = nil
	mu.Unlock()
	w.Destroy()
	return nil
}

func closeWindow() {
	mu.Lock()
	w := open
	mu.Unlock()
	if w != nil {
		// Terminate posts the quit message to the calling thread, so it has
		// to run on the window's own.
		w.Dispatch(w.Terminate)
	}
}
