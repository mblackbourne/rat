package rat

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ericfreese/otto"
	"github.com/nsf/termbox-go"
)

var (
	events        chan termbox.Event
	done          chan bool
	eventHandlers HandlerRegistry
	modes         map[string]Mode
	annotatorsDir string
	keyStack      []keyEvent
	vm            *otto.Otto

	widgets WidgetStack
	pagers  PagerStack
	gPrompt Prompt
)

func Init() error {
	if err := initTermbox(); err != nil {
		return err
	}

	events = make(chan termbox.Event)
	widgets = NewWidgetStack()
	pagers = NewPagerStack()
	done = make(chan bool)
	eventHandlers = NewHandlerRegistry()
	modes = make(map[string]Mode)
	vm = InitJs()

	widgets.Push(pagers)
	gPrompt = NewPrompt()

	AddEventHandler("C-c", Quit)

	loadJavascript()

	w, h := termbox.Size()
	layout(w, h)

	return nil
}

func closeTermbox() {
	termbox.Close()
}

func initTermbox() error {
	var err error

	if err = termbox.Init(); err != nil {
		return err
	}

	termbox.SetInputMode(termbox.InputAlt)
	termbox.SetOutputMode(termbox.Output256)

	return nil
}

func SetAnnotatorsDir(dir string) {
	annotatorsDir = dir
}

func loadJavascript() {
	if files, err := ioutil.ReadDir(ConfigDir); err == nil {
		for _, f := range files {
			if filepath.Ext(f.Name()) != ".js" || f.Name() == "rat.js" {
				continue
			}

			if file, openErr := os.Open(filepath.Join(ConfigDir, f.Name())); openErr == nil {
				evalJavascript(file)
				file.Close()
			}
		}
	}

	if file, err := os.Open(filepath.Join(ConfigDir, "rat.js")); err == nil {
		evalJavascript(file)
		file.Close()
	}
}

func evalJavascript(rd io.Reader) {
	if b, err := ioutil.ReadAll(rd); err == nil {
		_, err := vm.Run(string(b))
		if err != nil {
			panic(err)
		}
	}
}

func Close() {
	closeTermbox()
}

func Quit() {
	close(done)
}

func Run() {
	go func() {
		for {
			events <- termbox.PollEvent()
		}
	}()

loop:
	for {
		render()

		select {
		case <-done:
			break loop
		case e := <-events:
			switch e.Type {
			case termbox.EventKey:
				keyStack = append(keyStack, KeyEventFromTBEvent(&e))

				if handleEvent(keyStack) {
					keyStack = nil
				}
			case termbox.EventResize:
				layout(e.Width, e.Height)
			}
		case <-time.After(time.Second / 10):
		}
	}

	widgets.Destroy()
}

func AddChildPager(parent, child Pager, creatingKeys string) {
	pagers.AddChild(parent, child, creatingKeys)
}

func PushPager(p Pager) {
	pagers.Push(p)
}

func PopPager() {
	pagers.Pop()

	if pagers.Size() == 0 {
		Quit()
	}
}

func Confirm(message string, callback func()) {
	gPrompt.Confirm(message, callback)
}

func ConfirmExec(cmd string, ctx Context, callback func()) {
	Confirm(fmt.Sprintf("Run `%s`", cmd), func() {
		Exec(cmd, ctx)
		callback()
	})
}

func Exec(cmd string, ctx Context) {
	c := exec.Command(os.Getenv("SHELL"), "-c", cmd)

	c.Env = ContextEnvironment(ctx)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	closeTermbox()
	defer initTermbox()

	c.Run()
}

func RegisterMode(name string, mode Mode) {
	modes[name] = mode
}

func layout(width, height int) {
	widgets.SetBox(NewBox(0, 0, width, height-1))
	gPrompt.SetBox(NewBox(0, height-1, width, 1))
}

func render() {
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	widgets.Render()
	gPrompt.Render()
	termbox.Flush()
}

func AddEventHandler(keyStr string, handler func()) {
	eventHandlers.Add(KeySequenceFromString(keyStr), NewEventHandler(handler))
}

func handleEvent(ks []keyEvent) bool {
	if gPrompt.HandleEvent(ks) {
		return true
	}

	if widgets.HandleEvent(ks) {
		return true
	}

	if handler := eventHandlers.Find(ks); handler != nil {
		handler.Call(nil)
		return true
	}

	return false
}
