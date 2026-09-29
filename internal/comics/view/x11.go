package view

import (
	"errors"
	"fmt"
	"image"
	"math/bits"
	"os"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// X11 is a full-screen window on the X display (DISPLAY), drawn with
// PutImage: no GL, so it works with the W5F laptop's X without GLX.
type X11 struct {
	c       *xgb.Conn
	win     xproto.Window
	gc      xproto.Gcontext
	depth   byte
	size    image.Point
	keys    chan Key
	symsPer int
	minKey  xproto.Keycode
	syms    []xproto.Keysym
	shifts  [3]int // red, green, blue bit positions in a pixel
	buf     []byte
}

// OpenX11 opens the viewer window.
func OpenX11() (*X11, error) {
	if os.Getenv("DISPLAY") == "" {
		return nil, errors.New("no X display (DISPLAY is not set): the viewer runs in the W5F session")
	}
	c, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("cannot reach the X display: %w", err)
	}
	setup := xproto.Setup(c)
	scr := setup.DefaultScreen(c)
	x := &X11{c: c, depth: scr.RootDepth, size: image.Pt(int(scr.WidthInPixels), int(scr.HeightInPixels)), keys: make(chan Key, 32)}

	bpp := 0
	for _, f := range setup.PixmapFormats {
		if f.Depth == scr.RootDepth {
			bpp = int(f.BitsPerPixel)
		}
	}
	var vis *xproto.VisualInfo
	for _, d := range scr.AllowedDepths {
		for i := range d.Visuals {
			if d.Visuals[i].VisualId == scr.RootVisual {
				vis = &d.Visuals[i]
			}
		}
	}
	if bpp != 32 || vis == nil || setup.ImageByteOrder != xproto.ImageOrderLSBFirst {
		c.Close()
		return nil, fmt.Errorf("unsupported X screen (depth %d, %d bits per pixel): the viewer needs 24/32-bit colour", scr.RootDepth, bpp)
	}
	x.shifts = [3]int{bits.TrailingZeros32(vis.RedMask), bits.TrailingZeros32(vis.GreenMask), bits.TrailingZeros32(vis.BlueMask)}

	km, err := xproto.GetKeyboardMapping(c, setup.MinKeycode, byte(setup.MaxKeycode-setup.MinKeycode+1)).Reply()
	if err != nil {
		c.Close()
		return nil, err
	}
	x.symsPer, x.minKey, x.syms = int(km.KeysymsPerKeycode), setup.MinKeycode, km.Keysyms

	if x.win, err = xproto.NewWindowId(c); err != nil {
		c.Close()
		return nil, err
	}
	// Value order follows the mask bits: back pixel, override redirect, event mask.
	err = xproto.CreateWindowChecked(c, scr.RootDepth, x.win, scr.Root, 0, 0, uint16(x.size.X), uint16(x.size.Y), 0,
		xproto.WindowClassInputOutput, scr.RootVisual,
		xproto.CwBackPixel|xproto.CwOverrideRedirect|xproto.CwEventMask,
		[]uint32{scr.BlackPixel, 1, xproto.EventMaskKeyPress | xproto.EventMaskExposure}).Check()
	if err != nil {
		c.Close()
		return nil, err
	}
	if x.gc, err = xproto.NewGcontextId(c); err != nil {
		c.Close()
		return nil, err
	}
	xproto.CreateGC(c, x.gc, xproto.Drawable(x.win), 0, nil)
	xproto.MapWindow(c, x.win)
	// No window manager on the laptop: take the keyboard ourselves.
	for i := 0; i < 20; i++ {
		xproto.SetInputFocus(c, xproto.InputFocusParent, x.win, xproto.TimeCurrentTime)
		r, err := xproto.GrabKeyboard(c, true, x.win, xproto.TimeCurrentTime, xproto.GrabModeAsync, xproto.GrabModeAsync).Reply()
		if err == nil && r.Status == xproto.GrabStatusSuccess {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	go x.events()
	return x, nil
}

func (x *X11) Size() image.Point { return x.size }
func (x *X11) Keys() <-chan Key  { return x.keys }

func (x *X11) events() {
	defer close(x.keys)
	for {
		ev, err := x.c.WaitForEvent()
		if ev == nil && err == nil {
			return // connection closed
		}
		switch e := ev.(type) {
		case xproto.KeyPressEvent:
			if k := x.name(e.Detail, e.State); k != "" {
				x.keys <- k
			}
		case xproto.ExposeEvent:
			if e.Count == 0 {
				x.keys <- "expose"
			}
		}
	}
}

// name turns a key press into the viewer's key names.
func (x *X11) name(code xproto.Keycode, state uint16) Key {
	i := int(code-x.minKey) * x.symsPer
	if i < 0 || i >= len(x.syms) {
		return ""
	}
	sym := x.syms[i]
	if state&xproto.ModMaskShift != 0 && x.symsPer > 1 && x.syms[i+1] != 0 {
		sym = x.syms[i+1]
	}
	return keysymName(sym)
}

func keysymName(sym xproto.Keysym) Key {
	switch {
	case sym == 0x20:
		return "space"
	case sym >= 0x30 && sym <= 0x39, sym >= 0x61 && sym <= 0x7a, sym == 0x5b, sym == 0x5d:
		return Key(string(rune(sym)))
	case sym >= 0x41 && sym <= 0x5a: // shifted letters read as lowercase
		return Key(string(rune(sym + 0x20)))
	case sym >= 0xffb0 && sym <= 0xffb9: // keypad digits
		return Key(string(rune('0' + sym - 0xffb0)))
	case sym == 0x2bb, sym == 0x2ab: // ğ Ğ: where [ sits on a Turkish keyboard
		return "["
	case sym == 0xfc, sym == 0xdc: // ü Ü: where ] sits
		return "]"
	}
	return map[xproto.Keysym]Key{0xff1b: "esc", 0xff0d: "enter", 0xff8d: "enter", 0xff51: "left", 0xff52: "up",
		0xff53: "right", 0xff54: "down", 0xff55: "pgup", 0xff56: "pgdown", 0xff50: "home", 0xff57: "end"}[sym]
}

// Show puts a frame on the window in strips that fit one X request.
func (x *X11) Show(frame *image.RGBA) error {
	w, h := frame.Bounds().Dx(), frame.Bounds().Dy()
	if cap(x.buf) < w*h*4 {
		x.buf = make([]byte, w*h*4)
	}
	buf := x.buf[:w*h*4]
	rs, gs, bs := x.shifts[0], x.shifts[1], x.shifts[2]
	for p, o := 0, 0; p < len(frame.Pix); p, o = p+4, o+4 {
		px := uint32(frame.Pix[p])<<rs | uint32(frame.Pix[p+1])<<gs | uint32(frame.Pix[p+2])<<bs
		buf[o], buf[o+1], buf[o+2], buf[o+3] = byte(px), byte(px>>8), byte(px>>16), byte(px>>24)
	}
	rows := max(1, (64<<10)/(w*4))
	for y := 0; y < h; y += rows {
		n := min(rows, h-y)
		xproto.PutImage(x.c, xproto.ImageFormatZPixmap, xproto.Drawable(x.win), x.gc, uint16(w), uint16(n), 0, int16(y), 0, x.depth,
			buf[y*w*4:(y+n)*w*4])
	}
	_, err := xproto.GetInputFocus(x.c).Reply() // a round trip: the frame is on screen
	return err
}

// Close gives the keyboard back and removes the window.
func (x *X11) Close() error {
	xproto.UngrabKeyboard(x.c, xproto.TimeCurrentTime)
	xproto.DestroyWindow(x.c, x.win)
	xproto.GetInputFocus(x.c).Reply()
	x.c.Close()
	return nil
}
