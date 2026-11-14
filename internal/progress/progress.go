package progress

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type Stage interface {
	Update(message string)
	Done(result string)
}

type Tracker interface {
	Start(name string) Stage
}

type Options struct {
	Writer     io.Writer
	TTY        bool
	NoProgress bool
	Quiet      bool
	Actions    bool
}

type Reporter struct {
	options Options
	mu      sync.Mutex
}

type stage struct {
	reporter *Reporter
	name     string
	started  time.Time
	stop     chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	current  string
	finished bool
	once     sync.Once
}

type NoopStage struct{}

func (NoopStage) Update(string) {}
func (NoopStage) Done(string)   {}

func New(options Options) *Reporter {
	if options.Writer == nil {
		options.Writer = io.Discard
	}
	return &Reporter{options: options}
}

func (r *Reporter) Start(name string) Stage {
	if r.options.Quiet {
		return NoopStage{}
	}
	s := &stage{reporter: r, name: name, started: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	switch {
	case r.options.Actions:
		r.write("::group::" + escape(name))
	case r.options.TTY && !r.options.NoProgress:
		go s.spin()
	default:
		r.write(name + "...")
	}
	return s
}

func (r *Reporter) Message(message string) {
	if r.options.Quiet {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.options.TTY && !r.options.NoProgress && !r.options.Actions {
		_, _ = fmt.Fprint(r.options.Writer, "\r\x1b[K")
	}
	if r.options.Actions {
		message = escape(message)
	}
	_, _ = fmt.Fprintln(r.options.Writer, message)
}

func (s *stage) Update(message string) {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.current = message
	s.mu.Unlock()
	r := s.reporter
	if r.options.Actions || !r.options.TTY || r.options.NoProgress {
		if r.options.Actions {
			r.write(escape(message))
		} else {
			r.write(message)
		}
	}
}

func (s *stage) Done(result string) {
	s.once.Do(func() {
		s.mu.Lock()
		s.finished = true
		s.mu.Unlock()
		close(s.stop)
		if s.reporter.options.TTY && !s.reporter.options.NoProgress && !s.reporter.options.Actions {
			<-s.done
		}
		elapsed := time.Since(s.started).Round(100 * time.Millisecond)
		message := s.name
		if result != "" {
			message += ": " + result
		}
		message += " (" + elapsed.String() + ")"
		switch {
		case s.reporter.options.Actions:
			s.reporter.write(escape(message))
			s.reporter.write("::endgroup::")
		case s.reporter.options.TTY && !s.reporter.options.NoProgress:
			s.reporter.write("\r\x1b[K" + message)
		default:
			s.reporter.write(message)
		}
	})
}

func (s *stage) spin() {
	defer close(s.done)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	frames := []string{"|", "/", "-", "\\"}
	frame := 0
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			current := s.current
			finished := s.finished
			s.mu.Unlock()
			if finished {
				return
			}
			message := s.name
			if current != "" {
				message += " " + current
			}
			s.reporter.write("\r" + frames[frame%len(frames)] + " " + message)
			frame++
		}
	}
}

func (r *Reporter) write(message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = fmt.Fprintln(r.options.Writer, message)
}

func escape(message string) string {
	message = strings.ReplaceAll(message, "%", "%25")
	message = strings.ReplaceAll(message, "\r", "%0D")
	return strings.ReplaceAll(message, "\n", "%0A")
}
