// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package window

// WKWebView in an NSWindow, through the Objective-C runtime with purego.
// There is no cgo; AppKit and WebKit are loaded when the window opens.

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type nsRect struct{ X, Y, W, H float64 }
type nsSize struct{ W, H float64 }
type nsPoint struct{ X, Y float64 }

var (
	sel = objc.RegisterName

	mu     sync.Mutex
	window objc.ID // the open window, or 0
	app    objc.ID
)

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

func nsString(s string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), s)
}

// callBlock calls an Objective-C block that takes one object. A block
// starts with isa, flags and a reserved word, then its function, which
// takes the block itself first.
func callBlock(block, arg objc.ID) {
	if block == 0 {
		return
	}
	layout := *(*unsafe.Pointer)(unsafe.Pointer(&block))
	invoke := *(*uintptr)(unsafe.Add(layout, 16))
	purego.SyscallN(invoke, uintptr(block), uintptr(arg))
}

var registerOnce sync.Once
var delegateClass objc.Class
var registerErr error

// registerDelegate defines the class that answers for the application,
// the window and the web view.
func registerDelegate() (objc.Class, error) {
	registerOnce.Do(func() {
		var protocols []*objc.Protocol
		for _, p := range []string{"NSApplicationDelegate", "NSWindowDelegate", "WKUIDelegate"} {
			if pr := objc.GetProtocol(p); pr != nil {
				protocols = append(protocols, pr)
			}
		}
		delegateClass, registerErr = objc.RegisterClass("LeafbindDelegate", objc.GetClass("NSObject"), protocols, nil, []objc.MethodDef{
			{Cmd: sel("windowWillClose:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) { stop() }},
			// Quit from the menu ends the run loop, so the program can clean up,
			// rather than exiting on the spot.
			{Cmd: sel("applicationShouldTerminate:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) uint {
				closeWindow()
				return 0 // NSTerminateCancel
			}},
			// <input type="file">: WKWebView asks its delegate to show a panel.
			{Cmd: sel("webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:"),
				Fn: func(self objc.ID, _ objc.SEL, _, params, _, handler objc.ID) {
					panel := class("NSOpenPanel").Send(sel("openPanel"))
					panel.Send(sel("setAllowsMultipleSelection:"), objc.Send[bool](params, sel("allowsMultipleSelection")))
					panel.Send(sel("setCanChooseDirectories:"), false)
					panel.Send(sel("setCanChooseFiles:"), true)
					types := class("NSArray").Send(sel("arrayWithObject:"), nsString("pdf"))
					panel.Send(sel("setAllowedFileTypes:"), types)
					var urls objc.ID
					if objc.Send[int](panel, sel("runModal")) == 1 { // NSModalResponseOK
						urls = panel.Send(sel("URLs"))
					}
					callBlock(handler, urls)
				}},
			// A link that asks for a new window opens in the browser instead.
			{Cmd: sel("webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:"),
				Fn: func(self objc.ID, _ objc.SEL, _, _, action, _ objc.ID) objc.ID {
					url := action.Send(sel("request")).Send(sel("URL"))
					if url != 0 {
						class("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("openURL:"), url)
					}
					return 0
				}},
		})
	})
	return delegateClass, registerErr
}

func run(o Options) error {
	for _, fw := range []string{
		"/System/Library/Frameworks/AppKit.framework/AppKit",
		"/System/Library/Frameworks/WebKit.framework/WebKit",
	} {
		if _, err := purego.Dlopen(fw, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			return ErrUnavailable
		}
	}
	if !objc.Send[bool](class("NSThread"), sel("isMainThread")) {
		return ErrUnavailable // AppKit only works on the main thread
	}
	dc, err := registerDelegate()
	if err != nil {
		return err
	}
	pool := class("NSAutoreleasePool").Send(sel("new"))

	app = class("NSApplication").Send(sel("sharedApplication"))
	app.Send(sel("setActivationPolicy:"), 0) // regular: a Dock icon and a menu bar
	delegate := objc.ID(dc).Send(sel("new"))
	app.Send(sel("setDelegate:"), delegate)
	app.Send(sel("setMainMenu:"), mainMenu(o.Title))

	const titled, closable, miniaturizable, resizable = 1, 2, 4, 8
	w := class("NSWindow").Send(sel("alloc")).Send(sel("initWithContentRect:styleMask:backing:defer:"),
		nsRect{0, 0, float64(o.Width), float64(o.Height)}, uint(titled|closable|miniaturizable|resizable), uint(2), false)
	w.Send(sel("setTitle:"), nsString(o.Title))
	w.Send(sel("setReleasedWhenClosed:"), false)
	if o.MinW > 0 {
		w.Send(sel("setContentMinSize:"), nsSize{float64(o.MinW), float64(o.MinH)})
	}
	w.Send(sel("setDelegate:"), delegate)

	config := class("WKWebViewConfiguration").Send(sel("new"))
	view := class("WKWebView").Send(sel("alloc")).Send(sel("initWithFrame:configuration:"),
		nsRect{0, 0, float64(o.Width), float64(o.Height)}, config)
	view.Send(sel("setUIDelegate:"), delegate)
	w.Send(sel("setContentView:"), view)
	url := class("NSURL").Send(sel("URLWithString:"), nsString(o.URL))
	view.Send(sel("loadRequest:"), class("NSURLRequest").Send(sel("requestWithURL:"), url))

	w.Send(sel("center"))
	w.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
	app.Send(sel("activateIgnoringOtherApps:"), true)

	mu.Lock()
	window = w
	mu.Unlock()
	pool.Send(sel("drain"))

	app.Send(sel("run")) // until stop

	mu.Lock()
	window = 0
	mu.Unlock()
	return nil
}

// mainMenu builds the menu bar: the application menu with Quit, and the
// Edit menu, without which the usual shortcuts for cut, copy, paste and
// select all do nothing in the web view.
func mainMenu(title string) objc.ID {
	item := func(name, action, key string) objc.ID {
		return class("NSMenuItem").Send(sel("alloc")).Send(sel("initWithTitle:action:keyEquivalent:"),
			nsString(name), sel(action), nsString(key))
	}
	menu := func(name string, items ...objc.ID) objc.ID {
		m := class("NSMenu").Send(sel("alloc")).Send(sel("initWithTitle:"), nsString(name))
		for _, it := range items {
			m.Send(sel("addItem:"), it)
		}
		holder := class("NSMenuItem").Send(sel("new"))
		holder.Send(sel("setSubmenu:"), m)
		return holder
	}
	redo := item("Redo", "redo:", "z")
	redo.Send(sel("setKeyEquivalentModifierMask:"), uint(1<<17|1<<20)) // shift, command
	bar := class("NSMenu").Send(sel("new"))
	bar.Send(sel("addItem:"), menu(title,
		item("Hide "+title, "hide:", "h"),
		class("NSMenuItem").Send(sel("separatorItem")),
		item("Quit "+title, "terminate:", "q"),
	))
	bar.Send(sel("addItem:"), menu("Edit",
		item("Undo", "undo:", "z"), redo,
		class("NSMenuItem").Send(sel("separatorItem")),
		item("Cut", "cut:", "x"), item("Copy", "copy:", "c"), item("Paste", "paste:", "v"),
		item("Select All", "selectAll:", "a"),
	))
	bar.Send(sel("addItem:"), menu("Window",
		item("Minimize", "performMiniaturize:", "m"),
		item("Close", "performClose:", "w"),
	))
	return bar
}

// stop ends the run loop. It runs on the main thread, from the delegate;
// an event is posted so the loop notices at once.
func stop() {
	if app == 0 {
		return
	}
	app.Send(sel("stop:"), objc.ID(0))
	const applicationDefined = 15
	ev := class("NSEvent").Send(sel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
		uint(applicationDefined), nsPoint{}, uint(0), float64(0), 0, objc.ID(0), int16(0), 0, 0)
	app.Send(sel("postEvent:atStart:"), ev, true)
}

func closeWindow() {
	mu.Lock()
	w := window
	mu.Unlock()
	if w != 0 {
		// Safe from any thread: AppKit closes it on the main one, and the
		// delegate's windowWillClose: stops the loop.
		w.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("close"), objc.ID(0), false)
	}
}
