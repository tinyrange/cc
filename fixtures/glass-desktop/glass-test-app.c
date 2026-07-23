#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

static void record_event(const char *kind, unsigned long value) {
    FILE *file = fopen("/tmp/glass-events.jsonl", "a");
    if (file == NULL) {
        return;
    }
    fprintf(file, "{\"kind\":\"%s\",\"value\":%lu}\n", kind, value);
    fclose(file);
}

static void paint(Display *display, Window window, GC gc) {
    XSetForeground(display, gc, 0x202830);
    XFillRectangle(display, window, gc, 0, 0, 600, 400);
    XSetForeground(display, gc, 0xcc2828);
    XFillRectangle(display, window, gc, 20, 60, 160, 220);
    XSetForeground(display, gc, 0x28cc28);
    XFillRectangle(display, window, gc, 220, 60, 160, 220);
    XSetForeground(display, gc, 0x2828cc);
    XFillRectangle(display, window, gc, 420, 60, 160, 220);
    XSetForeground(display, gc, 0xffffff);
    XDrawString(display, window, gc, 20, 32, "cc glass input probe", 20);
    XFlush(display);
}

int main(void) {
    Display *display = XOpenDisplay(NULL);
    if (display == NULL) {
        fprintf(stderr, "glass-test-app: cannot open display\n");
        return 1;
    }
    int screen = DefaultScreen(display);
    XSetWindowAttributes attrs;
    memset(&attrs, 0, sizeof(attrs));
    attrs.override_redirect = True;
    attrs.background_pixel = 0x202830;
    Window window = XCreateWindow(
        display,
        RootWindow(display, screen),
        40, 40, 600, 400, 0,
        CopyFromParent, InputOutput, CopyFromParent,
        CWOverrideRedirect | CWBackPixel,
        &attrs);
    XStoreName(display, window, "cc glass input probe");
    XSelectInput(display, window, ExposureMask | KeyPressMask | ButtonPressMask);
    XMapRaised(display, window);
    XSetInputFocus(display, window, RevertToParent, CurrentTime);
    GC gc = XCreateGC(display, window, 0, NULL);
    paint(display, window, gc);

    for (;;) {
        XEvent event;
        XNextEvent(display, &event);
        if (event.type == Expose && event.xexpose.count == 0) {
            paint(display, window, gc);
        } else if (event.type == KeyPress) {
            KeySym keysym = XLookupKeysym(&event.xkey, 0);
            record_event("key", (unsigned long)keysym);
        } else if (event.type == ButtonPress) {
            record_event("button", (unsigned long)event.xbutton.button);
        }
    }
}
