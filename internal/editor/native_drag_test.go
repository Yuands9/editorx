package editor

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type nativeDragPaint struct {
	sequence int
	bounds   gfx.Rect
	viewport gfx.Size
	elapsed  time.Duration
	inputAt  time.Time
}

type nativeDragInput struct {
	kind     uint32
	sequence int
	bounds   gfx.Rect
	dragging bool
}

// Uses a private test desktop and messages sent only to this process's checked HWND.
// WM_PAINT returning includes the real Windows renderer/Present path, but an inactive
// desktop does not measure visible monitor scanout or physical mouse-to-photon latency.
func TestNativeSearchDrag(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_DRAG") != "1" {
		t.Skip("native drag check is opt-in")
	}
	scale, err := strconv.ParseFloat(os.Getenv("VIVA_SCALE"), 64)
	if err != nil || scale <= 0 {
		t.Fatal("runner must set VIVA_SCALE")
	}
	m := newEditorApp()
	raw := "[" + strings.Repeat(`{"name":"中文配置","enabled":true,"port":8080},`, 4999) + `{"name":"中文配置","enabled":true,"port":8080}]`
	source, _, err := transformJSON(raw, "格式化", 4)
	if err != nil {
		t.Fatal(err)
	}
	tab := m.addTab(document{saved: source, eol: "LF"}, "drag.json")
	tab.openSearch()
	tab.search.query.Insert("中文")
	var failure error
	var stats []string
	completed := false
	err = app.RunApplication(func(application *app.Application) error {
		page := ui.NewPage(ui.PageConfig{View: m.page, Theme: &m.theme})
		config := ui.Configure(page, app.Config{Title: "Viva Native Drag QA", Width: 1440, Height: 900, Invalidator: &m.invalidator})
		paints := make(chan nativeDragPaint, 4096)
		inputs := make(chan nativeDragInput, 4096)
		initial := make(chan nativeDragPaint, 1)
		var sequence int
		var received time.Time
		var scene nativeDragPaint
		var submittedSequence int
		paint := config.Paint
		config.Paint = func(canvas *gfx.Canvas, size gfx.Size) {
			paint(canvas, size)
			scene = nativeDragPaint{sequence: sequence, bounds: tab.search.bounds, viewport: size, inputAt: received}
			select {
			case initial <- scene:
			default:
			}
		}
		_, err := application.OpenWindow(config, new(gfx.Canvas))
		if err != nil {
			return err
		}
		hwnd, _, _ := editorUser.NewProc("GetActiveWindow").Call()
		var owner uint32
		editorUser.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if hwnd == 0 || owner != uint32(os.Getpid()) {
			application.Quit()
			return fmt.Errorf("test window PID verification failed")
		}
		callWindow := editorUser.NewProc("CallWindowProcW")
		setWindow := editorUser.NewProc("SetWindowLongPtrW")
		var original uintptr
		hook := syscall.NewCallback(func(window uintptr, kind uint32, first, second uintptr) uintptr {
			if kind == 0x200 && first&1 != 0 {
				sequence++
				received = time.Now()
			}
			result, _, _ := callWindow.Call(original, window, uintptr(kind), first, second)
			if kind == 0x200 && first&1 != 0 || kind == 0x201 || kind == 0x202 {
				inputs <- nativeDragInput{kind: kind, sequence: sequence, bounds: tab.search.bounds, dragging: tab.search.dragging}
			}
			if kind == 0x0F && scene.sequence > submittedSequence {
				sample := scene
				sample.elapsed = time.Since(sample.inputAt)
				submittedSequence = sample.sequence
				select {
				case paints <- sample:
				default:
				}
			}
			return result
		})
		original, _, err = setWindow.Call(hwnd, ^uintptr(3), hook)
		if original == 0 {
			application.Quit()
			return fmt.Errorf("install test window hook: %v", err)
		}
		go func() {
			first := <-initial
			send := editorUser.NewProc("SendMessageW")
			inject := func(kind uint32, point gfx.Point) (nativeDragInput, error) {
				x := int32(math.Round(float64(point.X) * scale))
				y := int32(math.Round(float64(point.Y) * scale))
				buttons := uintptr(1)
				if kind == 0x202 {
					buttons = 0
				}
				send.Call(hwnd, uintptr(kind), buttons, uintptr(uint32(uint16(x))|uint32(uint16(y))<<16))
				select {
				case result := <-inputs:
					if result.kind != kind {
						return result, fmt.Errorf("unexpected input message %x, wanted %x", result.kind, kind)
					}
					return result, nil
				case <-time.After(3 * time.Second):
					return nativeDragInput{}, fmt.Errorf("native input acknowledgement timeout")
				}
			}
			checkPaint := func(wanted nativeDragInput) ([]time.Duration, error) {
				var timings []time.Duration
				deadline := time.NewTimer(3 * time.Second)
				defer deadline.Stop()
				for {
					select {
					case frame := <-paints:
						if frame.sequence > 0 {
							timings = append(timings, frame.elapsed)
						}
						if frame.sequence >= wanted.sequence {
							if frame.bounds != wanted.bounds {
								return timings, fmt.Errorf("painted dialog position lagged input: frame=%+v input=%+v", frame.bounds, wanted.bounds)
							}
							return timings, nil
						}
					case <-deadline.C:
						return timings, fmt.Errorf("native paint did not reach input %d", wanted.sequence)
					}
				}
			}
			run := func() error {
				width, height := first.viewport.Width, first.viewport.Height
				if math.Abs(float64(first.bounds.X-(width-first.bounds.Width)/2)) > 1 || math.Abs(float64(first.bounds.Y-(height-first.bounds.Height)/2)) > 1 {
					return fmt.Errorf("dialog did not open centered: %+v in %+v", first.bounds, first.viewport)
				}
				bounds := first.bounds
				for _, mode := range []string{"paced-16ms", "burst"} {
					start := gfx.Point{X: bounds.X + 80, Y: bounds.Y + 8}
					pressed, e := inject(0x201, start)
					if e != nil {
						return e
					}
					if !pressed.dragging {
						return fmt.Errorf("title press did not capture drag")
					}
					var last nativeDragInput
					var timings []time.Duration
					for index := 1; index <= 48; index++ {
						point := gfx.Point{X: start.X + float32(index%24)*3, Y: start.Y + float32(index%12)*2}
						last, e = inject(0x200, point)
						if e != nil {
							return e
						}
						want := gfx.Rect{X: bounds.X + point.X - start.X, Y: bounds.Y + point.Y - start.Y, Width: bounds.Width, Height: bounds.Height}
						if !last.dragging || last.bounds != want {
							return fmt.Errorf("native drag lost cursor at %d: %+v, want %+v", index, last, want)
						}
						if mode == "paced-16ms" {
							time.Sleep(16 * time.Millisecond)
						}
					}
					more, e := checkPaint(last)
					if e != nil {
						return e
					}
					timings = append(timings, more...)
					released, e := inject(0x202, gfx.Point{X: start.X, Y: start.Y})
					if e != nil {
						return e
					}
					if released.dragging {
						return fmt.Errorf("release retained dragging")
					}
					bounds = released.bounds
					slices.Sort(timings)
					if len(timings) == 0 {
						return fmt.Errorf("native renderer produced no drag frames")
					}
					stats = append(stats, fmt.Sprintf("scale=%g mode=%s events=48 submittedFrames=%d latest-input-to-WM_PAINT-return p50=%s p95=%s", scale, mode, len(timings), timings[len(timings)/2], timings[(len(timings)-1)*95/100]))
					for len(paints) > 0 {
						<-paints
					}
				}
				// Drag past the title and both client edges while capture is held, then
				// start a second drag from the new visible title position.
				start := gfx.Point{X: bounds.X + 8, Y: bounds.Y + 8}
				if _, e := inject(0x201, start); e != nil {
					return e
				}
				last, e := inject(0x200, gfx.Point{X: -200, Y: -200})
				if e != nil {
					return e
				}
				if !last.dragging || last.bounds.X != 12 || last.bounds.Y != 12 {
					return fmt.Errorf("captured edge drag failed: %+v", last)
				}
				if _, e = checkPaint(last); e != nil {
					return e
				}
				if _, e = inject(0x202, gfx.Point{X: -200, Y: -200}); e != nil {
					return e
				}
				if _, e = inject(0x201, gfx.Point{X: 20, Y: 20}); e != nil {
					return e
				}
				last, e = inject(0x200, gfx.Point{X: 80, Y: 60})
				if e != nil {
					return e
				}
				if !last.dragging || last.bounds.X != 72 || last.bounds.Y != 52 {
					return fmt.Errorf("second edge drag failed: %+v", last)
				}
				if _, e = checkPaint(last); e != nil {
					return e
				}
				if _, e = inject(0x202, gfx.Point{X: 80, Y: 60}); e != nil {
					return e
				}
				return nil
			}
			result := run()
			application.Post(func() {
				setWindow.Call(hwnd, ^uintptr(3), original)
				failure = result
				completed = true
				application.Quit()
			})
		}()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !completed {
		t.Fatal("native drag loop ended early")
	}
	if failure != nil {
		t.Fatal(failure)
	}
	for _, line := range stats {
		t.Log(line)
	}
	t.Log("Native WM input, DPI scaling, centered dialog, continuous title capture, edge re-drag and painted latest position passed; inactive desktop excludes real monitor scanout latency.")
}
