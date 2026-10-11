// closewin.c — send a real WM_DELETE_WINDOW close request to a window.
//
// This is the exact ClientMessage a window manager delivers when the user
// closes the window through the WM: the WM_PROTOCOLS / WM_DELETE_WINDOW
// pair. The app is expected to handle it (Qt maps it to QCloseEvent →
// quit) and exit cleanly — a forced kill would NOT be clean shutdown.
//
// Build: cc -O2 -o closewin closewin.c -lX11
#include <X11/Xlib.h>
#include <X11/Xatom.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main(int argc, char **argv) {
    if (argc != 2) { fprintf(stderr, "usage: closewin <window-id>\n"); return 2; }

    Window win = (Window)strtoul(argv[1], NULL, 0);
    Display *d = XOpenDisplay(NULL);
    if (!d) { fprintf(stderr, "closewin: cannot open display\n"); return 1; }

    Atom wmProtocols = XInternAtom(d, "WM_PROTOCOLS", False);
    Atom wmDelete = XInternAtom(d, "WM_DELETE_WINDOW", False);

    XEvent ev;
    memset(&ev, 0, sizeof(ev));
    ev.xclient.type = ClientMessage;
    ev.xclient.window = win;
    ev.xclient.message_type = wmProtocols;
    ev.xclient.format = 32;
    ev.xclient.data.l[0] = (long)wmDelete;
    ev.xclient.data.l[1] = CurrentTime;

    XSendEvent(d, win, False, NoEventMask, &ev);
    XFlush(d);
    XCloseDisplay(d);
    return 0;
}
