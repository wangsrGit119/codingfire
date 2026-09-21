//go:build darwin && !ios

// The macOS half of the overlay-window layer, written against AppKit directly.
//
// go-gui's metal backend gives us a borderless transparent window but keeps its
// NSWindow private, and it has no always-on-top, no click-through and no
// move/resize — the same three gaps the Win32 shim fills with user32. There is
// no second toolkit here to reach through, so the calls are AppKit's own.
//
// Threading is the one thing that makes this file different from the other two
// platforms. AppKit requires every NSApplication/NSWindow call on thread 0, and
// this process's thread 0 is where the Go main goroutine lives — go-gui pins it
// there from an init() precisely so AppKit stops aborting with "API misuse:
// setting the main menu on a non-main thread" (see
// gui/backend/metal/mainthread.go). The event loop that services the main
// dispatch queue runs on that same thread.
//
// Our callers are mostly *not* on it: the 20 Hz tick goroutine polls the window
// rectangle and re-resolves window handles. So:
//
//   - mutations are dispatched asynchronously to the main queue and return
//     immediately, and the Go side keeps its own copy of the frame it asked for
//     (see darwinFrames in win32_darwin.go);
//   - reads hop to the main queue synchronously, so the caller always sees a
//     settled answer rather than a torn one.

#import <AppKit/AppKit.h>
#import <dispatch/dispatch.h>
#include <string.h>

#include "cocoa_darwin.h"

// cfOnMainThread answers "am I on the thread AppKit wants", which is the
// process's main thread — thread 0, the one go-gui pins the Go main goroutine
// to. [NSThread isMainThread] is used rather than pthread_main_np() because it
// comes from Foundation, which this file already depends on for everything
// else, instead of from a libc declaration whose visibility depends on
// __DARWIN_C_LEVEL.
int cfOnMainThread(void) {
    return [NSThread isMainThread] ? 1 : 0;
}

// cfRunOnMain runs a block on the main thread, inline when the caller is already
// there — dispatch_sync onto the queue you are already on deadlocks.
//
// The NSApp==nil case is not a shortcut: with no application object there is no
// window list to read, so the block's own nil checks already produce the right
// answer, and hopping would hang a headless run (--dump, --render) where no run
// loop is running to service the main queue.
static void cfRunOnMain(void (^block)(void)) {
    if (cfOnMainThread() || NSApp == nil) {
        block();
        return;
    }
    dispatch_sync(dispatch_get_main_queue(), block);
}

// cfWindowFromHandle turns the uintptr_t handle the Go side passes around back
// into an NSWindow.
//
// The double cast is required rather than stylistic: __bridge only converts
// between Objective-C and Core Foundation types, so an integer has to become a
// void * on the way. `(__bridge NSWindow *)win` is a compile error under ARC —
// "incompatible types casting 'uintptr_t' to 'NSWindow *' with a __bridge
// cast".
//
// The result is not retained. AppKit owns the window and outlives our use of
// the handle; the mutation blocks below capture it strongly for their own
// duration.
static NSWindow *cfWindowFromHandle(uintptr_t win) {
    return (__bridge NSWindow *)(void *)win;
}

// --- Window lookup ---

// appWindows is the process's window list, or nil when the application object
// does not exist yet.
//
// NSApp is nil until something on the main thread creates it, which the metal
// backend does in metalAppInit before any window exists. Returning nil rather
// than calling +sharedApplication keeps a background caller from creating the
// application object off the main thread.
static NSArray<NSWindow *> *appWindows(void) {
    NSApplication *app = NSApp;
    if (app == nil) {
        return nil;
    }
    return [app windows];
}

uintptr_t cfWindowFindByTitle(const char *title) {
    if (title == NULL) {
        return 0;
    }
    __block uintptr_t found = 0;
    cfRunOnMain(^{
        NSArray<NSWindow *> *windows = appWindows();
        if (windows == nil) {
            return;
        }
        NSString *want = [NSString stringWithUTF8String:title];
        if (want == nil) {
            return;
        }
        for (NSWindow *w in windows) {
            if ([[w title] isEqualToString:want]) {
                // Non-owning: AppKit owns the window and outlives our use of the
                // handle, which is why __bridge is the right cast under ARC.
                found = (uintptr_t)(__bridge void *)w;
                return;
            }
        }
    });
    return found;
}

int cfWindowIsAlive(uintptr_t win) {
    if (win == 0) {
        return 0;
    }
    __block int alive = 0;
    cfRunOnMain(^{
        NSArray<NSWindow *> *windows = appWindows();
        if (windows == nil) {
            return;
        }
        // Membership rather than a bare pointer test: a destroyed window leaves
        // the address free for reuse, and a stale handle must not be mistaken
        // for a live one.
        NSWindow *want = cfWindowFromHandle(win);
        for (NSWindow *w in windows) {
            if (w == want) {
                alive = 1;
                return;
            }
        }
    });
    return alive;
}

int cfWindowIsVisible(uintptr_t win) {
    if (win == 0) {
        return 0;
    }
    __block int visible = 0;
    cfRunOnMain(^{
        NSWindow *w = cfWindowFromHandle(win);
        visible = [w isVisible] ? 1 : 0;
    });
    return visible;
}

int cfWindowCount(void) {
    __block int count = 0;
    cfRunOnMain(^{
        NSArray<NSWindow *> *windows = appWindows();
        count = windows == nil ? 0 : (int)[windows count];
    });
    return count;
}

uintptr_t cfWindowAt(int index) {
    if (index < 0) {
        return 0;
    }
    __block uintptr_t out = 0;
    cfRunOnMain(^{
        NSArray<NSWindow *> *windows = appWindows();
        if (windows == nil || index >= (int)[windows count]) {
            return;
        }
        out = (uintptr_t)(__bridge void *)[windows objectAtIndex:(NSUInteger)index];
    });
    return out;
}

int cfWindowTitleAt(int index, char *buf, int cap) {
    if (buf == NULL || cap <= 0) {
        return -1;
    }
    buf[0] = '\0';
    if (index < 0) {
        return -1;
    }
    __block int out = -1;
    cfRunOnMain(^{
        NSArray<NSWindow *> *windows = appWindows();
        if (windows == nil || index >= (int)[windows count]) {
            return;
        }
        NSString *title = [[windows objectAtIndex:(NSUInteger)index] title];
        if (title == nil) {
            out = 0;
            return;
        }
        const char *utf8 = [title UTF8String];
        if (utf8 == NULL) {
            out = 0;
            return;
        }
        size_t n = strlen(utf8);
        if ((int)n + 1 > cap) {
            return;
        }
        memcpy(buf, utf8, n + 1);
        out = (int)n;
    });
    return out;
}

void cfWindowFrame(uintptr_t win, double *out) {
    if (out == NULL) {
        return;
    }
    out[0] = out[1] = out[2] = out[3] = 0;
    if (win == 0) {
        return;
    }
    cfRunOnMain(^{
        NSRect f = [cfWindowFromHandle(win) frame];
        out[0] = f.origin.x;
        out[1] = f.origin.y;
        out[2] = f.size.width;
        out[3] = f.size.height;
    });
}

// --- Window mutation ---
//
// Each of these captures the window strongly: ARC retains it for the block, so
// a window that is destroyed before the block runs is kept alive until it has
// rather than left dangling.

void cfWindowSetFrame(uintptr_t win, double x, double y, double w, double h) {
    if (win == 0) {
        return;
    }
    NSWindow *target = cfWindowFromHandle(win);
    dispatch_async(dispatch_get_main_queue(), ^{
        [target setFrame:NSMakeRect(x, y, w, h) display:YES];
    });
}

void cfWindowSetLevel(uintptr_t win, int floating) {
    if (win == 0) {
        return;
    }
    NSWindow *target = cfWindowFromHandle(win);
    dispatch_async(dispatch_get_main_queue(), ^{
        // NSFloatingWindowLevel sits above ordinary windows but below menus and
        // alerts, which is the closest analogue of HWND_TOPMOST: the campfire
        // floats over the desktop without covering a dialog the user is reading.
        [target setLevel:(floating ? NSFloatingWindowLevel : NSNormalWindowLevel)];
    });
}

void cfWindowSetIgnoresMouse(uintptr_t win, int ignore) {
    if (win == 0) {
        return;
    }
    NSWindow *target = cfWindowFromHandle(win);
    dispatch_async(dispatch_get_main_queue(), ^{
        [target setIgnoresMouseEvents:(ignore ? YES : NO)];
    });
}

void cfWindowSetVisible(uintptr_t win, int visible) {
    if (win == 0) {
        return;
    }
    NSWindow *target = cfWindowFromHandle(win);
    dispatch_async(dispatch_get_main_queue(), ^{
        if (visible) {
            // orderFront, not makeKeyAndOrderFront: showing the campfire must
            // not steal focus from whatever the user is typing into.
            [target orderFront:nil];
        } else {
            [target orderOut:nil];
        }
    });
}

void cfWindowActivate(uintptr_t win) {
    if (win == 0) {
        return;
    }
    NSWindow *target = cfWindowFromHandle(win);
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp activateIgnoringOtherApps:YES];
        [target makeKeyAndOrderFront:nil];
    });
}

void cfWindowSetOverlayBehavior(uintptr_t win, int overlay) {
    if (win == 0) {
        return;
    }
    NSWindow *target = cfWindowFromHandle(win);
    dispatch_async(dispatch_get_main_queue(), ^{
        NSWindowCollectionBehavior b = [target collectionBehavior];
        NSWindowCollectionBehavior flags =
            NSWindowCollectionBehaviorCanJoinAllSpaces |
            NSWindowCollectionBehaviorStationary |
            NSWindowCollectionBehaviorIgnoresCycle;
        if (overlay) {
            // A desktop overlay belongs on every Space, stays put when Spaces
            // switch, and has no business appearing in the window cycle.
            b |= flags;
        } else {
            b &= ~flags;
        }
        [target setCollectionBehavior:b];
    });
}

// --- Screens and pointer ---

double cfPrimaryScreenHeight(void) {
    __block double height = 0;
    cfRunOnMain(^{
        NSArray<NSScreen *> *screens = [NSScreen screens];
        if ([screens count] == 0) {
            return;
        }
        // screens[0] is the display with the menu bar, and its bottom-left
        // corner is the origin of Cocoa's screen coordinate space. +mainScreen
        // is NOT the same thing: it follows the keyboard focus.
        height = [[screens objectAtIndex:0] frame].size.height;
    });
    return height;
}

static void copyRect(NSRect r, double *out) {
    out[0] = r.origin.x;
    out[1] = r.origin.y;
    out[2] = r.size.width;
    out[3] = r.size.height;
}

static NSScreen *screenAt(double x, double y) {
    for (NSScreen *s in [NSScreen screens]) {
        if (NSPointInRect(NSMakePoint(x, y), [s frame])) {
            return s;
        }
    }
    return nil;
}

static NSScreen *primaryScreen(void) {
    NSArray<NSScreen *> *screens = [NSScreen screens];
    if ([screens count] == 0) {
        return nil;
    }
    return [screens objectAtIndex:0];
}

void cfScreenVisibleFrame(double *out) {
    if (out == NULL) {
        return;
    }
    out[0] = out[1] = out[2] = out[3] = 0;
    cfRunOnMain(^{
        NSScreen *s = primaryScreen();
        if (s == nil) {
            return;
        }
        // visibleFrame excludes the menu bar and the Dock, which is what
        // "bottom right" has to mean if the campfire is not to sit under the
        // clock.
        copyRect([s visibleFrame], out);
    });
}

void cfScreenVisibleFrameAt(double x, double y, double *out) {
    if (out == NULL) {
        return;
    }
    out[0] = out[1] = out[2] = out[3] = 0;
    cfRunOnMain(^{
        NSScreen *s = screenAt(x, y);
        if (s == nil) {
            s = primaryScreen();
        }
        if (s == nil) {
            return;
        }
        copyRect([s visibleFrame], out);
    });
}

double cfScreenScaleAt(double x, double y) {
    __block double scale = 1;
    cfRunOnMain(^{
        NSScreen *s = screenAt(x, y);
        if (s == nil) {
            s = primaryScreen();
        }
        if (s == nil) {
            return;
        }
        scale = [s backingScaleFactor];
    });
    return scale;
}

void cfCursorPos(double *out) {
    if (out == NULL) {
        return;
    }
    cfRunOnMain(^{
        // Cocoa coordinates: origin at the bottom-left of the menu-bar display,
        // y increasing upward. The Go side flips it.
        NSPoint p = [NSEvent mouseLocation];
        out[0] = p.x;
        out[1] = p.y;
    });
}

int cfLeftButtonDown(void) {
    __block int down = 0;
    cfRunOnMain(^{
        down = ([NSEvent pressedMouseButtons] & 0x1) ? 1 : 0;
    });
    return down;
}

// --- Shell ---

int cfOpenURL(const char *url) {
    if (url == NULL) {
        return 0;
    }
    NSString *s = [NSString stringWithUTF8String:url];
    if (s == nil) {
        return 0;
    }
    NSURL *u = [NSURL URLWithString:s];
    if (u == nil) {
        return 0;
    }
    // openURL: hands the scheme to whatever the user has registered for it, and
    // is documented as callable from any thread — so unlike everything above it
    // needs no hop.
    return [[NSWorkspace sharedWorkspace] openURL:u] ? 1 : 0;
}
