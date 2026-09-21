//go:build darwin && !ios

// AppKit bridge for the overlay-window layer. Implemented in cocoa_darwin.m;
// the Go side is win32_darwin.go.
//
// AppKit is a main-thread API and most callers here are on the 20 Hz tick
// goroutine, so threading is explicit:
//
//   - every function that mutates a window dispatches to the main queue and
//     returns immediately, and the Go side keeps its own copy of the frame it
//     asked for, which is what makes a fire-and-forget mutation usable by a
//     polling caller;
//   - every function that reads AppKit state hops to the main queue and waits,
//     so the caller gets a settled answer instead of a torn one. Calling one of
//     these from the main thread itself is free — see cfRunOnMain in the .m.

#ifndef CODINGFIRE_COCOA_DARWIN_H
#define CODINGFIRE_COCOA_DARWIN_H

#include <stdint.h>

// --- Threading ---

// cfOnMainThread reports whether the caller is the main thread.
int cfOnMainThread(void);

// --- Window lookup ---

// cfWindowFindByTitle returns the first window whose title matches, or 0.
uintptr_t cfWindowFindByTitle(const char *title);
int       cfWindowIsAlive(uintptr_t win);
int       cfWindowIsVisible(uintptr_t win);

// cfWindowCount and cfWindowTitleAt enumerate the process's windows.
// cfWindowTitleAt writes a NUL-terminated UTF-8 title into buf and returns its
// byte length, or -1 when the index is out of range or the buffer is too small.
int       cfWindowCount(void);
uintptr_t cfWindowAt(int index);
int       cfWindowTitleAt(int index, char *buf, int cap);

// cfWindowFrame writes x, y, width, height (Cocoa coordinates) into out.
void cfWindowFrame(uintptr_t win, double *out);

// --- Window mutation (dispatched to the main queue) ---

void cfWindowSetFrame(uintptr_t win, double x, double y, double w, double h);
void cfWindowSetLevel(uintptr_t win, int floating);
void cfWindowSetIgnoresMouse(uintptr_t win, int ignore);
void cfWindowSetVisible(uintptr_t win, int visible);
void cfWindowActivate(uintptr_t win);
void cfWindowSetOverlayBehavior(uintptr_t win, int overlay);

// --- Screens and pointer ---

// cfPrimaryScreenHeight is the menu-bar display's height, which is what the
// Cocoa-to-top-left coordinate conversion is measured from.
double cfPrimaryScreenHeight(void);

// cfScreenVisibleFrame writes the primary display's visibleFrame (Cocoa
// coordinates: x, y, width, height) into out.
void cfScreenVisibleFrame(double *out);

// cfScreenVisibleFrameAt writes the visibleFrame of the display containing a
// Cocoa-coordinate point, falling back to the primary display.
void cfScreenVisibleFrameAt(double x, double y, double *out);

// cfScreenScaleAt returns the backing scale factor of the display containing a
// Cocoa-coordinate point, falling back to the primary display.
double cfScreenScaleAt(double x, double y);

// cfCursorPos writes the mouse position in Cocoa coordinates into out.
void cfCursorPos(double *out);

// cfLeftButtonDown reports whether the primary mouse button is held.
int cfLeftButtonDown(void);

// --- Shell ---

int cfOpenURL(const char *url);

#endif
