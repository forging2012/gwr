// Copyright (c) 2016 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package tap

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/uber-go/gwr"
	"github.com/uber-go/gwr/source"
)

const (
	defaultName = "/tap/trace"
	namePattern = "/tap/trace/%s"
)

// Tracer implements a gwr data source that allows easy tracing of scope data,
// such as function calls, or rounds of a worker goroutine's loop.
//
// Tracers should be created for each area of the application that can be
// traced.  This could be as simple as creating a package-level tracer:
//
//     package foo
//
//     import "github.com/uber-go/gwr/source"
//
//     tracer := source.AddNewTracer("foo")
//
// Tracers can also be attached to parts of the application:
//
//     type Thing struct {
//         t *Tracer
//     }
//
//     func NewThing() *Thing {
//         // ...
//         t.tracer = source.AddNewTracer(fmt.Sprintf("foo/%v", someThingIdentifier))
//         // ...
//     }
//
// If Things are not the same life-cycle as the application, then they should
// have teardown code to remove their tracer data sources:
//
//     gwr.DefaultDataSources.Remove(t.tracer.Name())
//
// You can then proceed to trace your functions and methods.  First decide
// where/what you want to start tracing.  this will probably be one or more
// exported functions or methods called by your user.
//
// Within these entry points use Tracer.Scope to create a root scope.  Pass
// this scope along to any called functions that you want to trace.  Functions
// that get passed a scope should start off by calling scope.Sub to create
// their own scope.
//
// Within a traced function, start off by calling scope.OpenCall to note the
// start of function.  While doing normal work within a traced function, you
// may call scope.Info to note log any additional data.  Finally once done the
// traced function should call one of scope.CloseCall, scope.Error or
// scope.ErrorName.
//
// If a traced function has more than one possible way to error, it should use
// scope.ErrorName to describe what failed.  Furthermore, a traced function may
// call scope.ErrorName more than once if it has recoverable errors, or
// otherwise makes progress around errors.
//
// You can similarly trace a worker goroutine:
//
//     ch := make(chan int)
//     go func() {
//         for n := range ch {
//             scope := tracer.Scope("n <- workerChan").Open(n)
//             // do something...
//             scope.Close()
//         }
//     }()
type Tracer struct {
	name    string
	watcher source.GenericDataWatcher
}

// NewTracer creates a Tracer with a given name.
func NewTracer(name string) *Tracer {
	name = fmt.Sprintf(namePattern, name)
	return &Tracer{
		name: name,
	}
}

// AddNewTracer creates a new tracer and adds it to the default gwr sources.
// It panics if the given name is already defined.
func AddNewTracer(name string) *Tracer {
	src := NewTracer(name)
	if err := gwr.AddGenericDataSource(src); err != nil {
		panic(err.Error())
	}
	return src
}

func (src *Tracer) emit(item interface{}) bool {
	if src.watcher == nil {
		return false
	}
	return src.watcher.HandleItem(item)
}

// Active returns true if there any watchers; when not active, all emitted data
// is dropped.  This should be used by call sites to control scope creation.
func (src *Tracer) Active() bool {
	return src.watcher != nil && src.watcher.Active()
}

// Name returns the gwr source name of the tracer.
func (src *Tracer) Name() string {
	return src.name
}

// Formats returns tracer-specific formats.
func (src *Tracer) Formats() map[string]source.GenericDataFormat {
	return map[string]source.GenericDataFormat{
		"text": defaultTextFormat,
	}
}

// SetWatcher sets the current watcher.
func (src *Tracer) SetWatcher(watcher source.GenericDataWatcher) {
	src.watcher = watcher
}

// Scope creates a new named trace scope
func (src *Tracer) Scope(name string) *TraceScope {
	return newScope(src, nil, name)
}

// MaybeScope creates a new named scope if the tracer is active; otherwise nil
// is returned.
func (src *Tracer) MaybeScope(name string) *TraceScope {
	if !src.Active() {
		return nil
	}
	return newScope(src, nil, name)
}

// DefaultTracer is available for easy scope logging without needing to create
// a separate tracer.
var DefaultTracer = Tracer{
	name: "/tap/trace",
}

// Active returns whether the default tracer is active.
func Active() bool {
	return DefaultTracer.Active()
}

// Scope creates a new scope on the default tracer.
func Scope(name string) *TraceScope {
	return DefaultTracer.Scope(name)
}

// MaybeScope creates a new scope on the default tracer, if it is active;
// otherwise nil is returned.
func MaybeScope(name string) *TraceScope {
	return DefaultTracer.MaybeScope(name)
}

// TODO: better do this
var lastTraceId uint64

// ResetTraceID resets the last trace id; this is intended to be used only for
// test stability.
func ResetTraceID() {
	atomic.StoreUint64(&lastTraceId, 0)
}

// TraceScope represents a traced scope, such as a function call, or an
// iteration of a worker goroutine loop.
type TraceScope struct {
	trc    *Tracer
	top    *TraceScope
	parent *TraceScope
	id     uint64
	name   string
	begin  time.Time
	end    time.Time
}

func newScope(trc *Tracer, parent *TraceScope, name string) *TraceScope {
	sc := &TraceScope{
		trc:    trc,
		parent: parent,
		id:     atomic.AddUint64(&lastTraceId, 1),
		name:   name,
	}
	if parent != nil {
		sc.top = parent.top
	} else {
		sc.top = sc
	}
	return sc
}

// Active returns true if the tracer is active, false otherwise.
func (sc *TraceScope) Active() bool {
	return sc.trc.Active()
}

// Root returns the root scope.
func (sc *TraceScope) Root() *TraceScope {
	return sc.top
}

// Parent returns the parent scope; this is nil for root scopes..
func (sc *TraceScope) Parent() *TraceScope {
	return sc.parent
}

// BeginTime returns the time of the first scope.Open or scope.OpenCall (only
// one of these should be called, but the first one wins for begin time
// anyhow).  Sub-scope times do not affect their parent scope's begin/end.
func (sc *TraceScope) BeginTime() time.Time {
	return sc.begin
}

// EndTime returns the time of the last scope.Close, scope.CloseCall,
// scope.Error, or scope.ErrorName.  Sub-scope times do not affect their parent
// scope's begin/end.
func (sc *TraceScope) EndTime() time.Time {
	return sc.end
}

// Sub opens and returns a new sub-scope.
func (sc *TraceScope) Sub(name string) *TraceScope {
	return newScope(sc.trc, sc, name)
}

// Info emits an info record with the passed arguments
func (sc *TraceScope) Info(args ...interface{}) *TraceScope {
	return sc.emitRecord(infoRecord, genericArgs(args))
}

// Open emits a begin record with the given arguments.
func (sc *TraceScope) Open(args ...interface{}) *TraceScope {
	return sc.emitRecord(beginRecord, genericArgs(args))
}

// Error emits an error record with the given error and arguments.
func (sc *TraceScope) Error(err error, args ...interface{}) *TraceScope {
	return sc.ErrorName("", err, args...)
}

// ErrorName emits an error record with the given error and arguments.
func (sc *TraceScope) ErrorName(name string, err error, args ...interface{}) *TraceScope {
	return sc.emitRecord(errRecord, errArgs{name, err, genericArgs(args)})
}

// Close emits a end record with the given arguments.
func (sc *TraceScope) Close(args ...interface{}) *TraceScope {
	return sc.emitRecord(endRecord, genericArgs(args))
}

// OpenCall emits a begin record for a function call with the given arguments.
func (sc *TraceScope) OpenCall(args ...interface{}) *TraceScope {
	return sc.emitRecord(beginRecord, callArgs(args))
}

// CloseCall emits an end record for a function call with the return values.
func (sc *TraceScope) CloseCall(rets ...interface{}) *TraceScope {
	return sc.emitRecord(endRecord, callRets(rets))
}

func (sc *TraceScope) emitRecord(t recordType, args interface{}) *TraceScope {
	now := time.Now()
	switch t {
	case beginRecord:
		if sc.begin.IsZero() {
			sc.begin = now
		}
	case endRecord:
		fallthrough
	case errRecord:
		if sc.end.IsZero() || now.After(sc.end) {
			sc.end = now
		}
	}
	rec := record{
		Time:    now,
		Type:    t,
		ScopeId: sc.top.id,
		SpanId:  sc.id,
		Name:    sc.name,
		Args:    args,
	}
	if sc.parent != nil {
		rec.ParentId = &sc.parent.id
	}
	sc.trc.emit(&rec)
	return sc
}

func dumpArgs(args []interface{}) string {
	// TODO: replace / make better; consider using go-spew
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = fmt.Sprintf("%v", arg)
	}
	return strings.Join(parts, ", ")
}

type recordType uint

const (
	beginRecord recordType = iota
	infoRecord
	endRecord
	errRecord
)

func (t recordType) String() string {
	switch t {
	case beginRecord:
		return "begin"
	case infoRecord:
		return "info"
	case endRecord:
		return "end"
	case errRecord:
		return "error"
	default:
		return fmt.Sprintf("UNK(%d)", int(t))
	}
}

func (t recordType) MarkString() string {
	switch t {
	case beginRecord:
		return "-->"
	case infoRecord:
		return "..."
	case endRecord:
		return "<--"
	case errRecord:
		return "!!!"
	default:
		return fmt.Sprintf("UNK(%d)", int(t))
	}
}

type genericArgs []interface{}

func (args genericArgs) String() string {
	return dumpArgs(args)
}

type callArgs []interface{}

func (args callArgs) String() string {
	return dumpArgs(args)
}

type callRets []interface{}

func (args callRets) String() string {
	return dumpArgs(args)
}

type errArgs struct {
	name  string
	err   error
	extra genericArgs
}

func (args errArgs) String() string {
	var s string
	if args.name != "" {
		s = fmt.Sprintf("%s Error(%s)", args.name, args.err)
	} else {
		s = fmt.Sprintf("Error(%s)", args.err)
	}
	if len(args.extra) > 0 {
		s = fmt.Sprintf("%s %s", s, args.extra)
	}
	return s
}

type record struct {
	Time     time.Time   `json:"time"`
	Type     recordType  `json:"type"`
	ScopeId  uint64      `json:"scope_id"`
	SpanId   uint64      `json:"span_id"`
	ParentId *uint64     `json:"parent_id"`
	Name     string      `json:"name"`
	Args     interface{} `json:"args"`
}

func (rec record) IDString() string {
	if rec.ParentId == nil {
		return fmt.Sprintf("%v::%v", rec.ScopeId, rec.SpanId)
	}
	return fmt.Sprintf("%v:%v:%v", rec.ScopeId, *rec.ParentId, rec.SpanId)
}

func (rec record) String() string {
	switch rec.Args.(type) {
	case callArgs:
		return fmt.Sprintf("%s %s [%s] %s(%s)",
			rec.Type.MarkString(), rec.Time, rec.IDString(),
			rec.Name, rec.Args)
	case callRets:
		return fmt.Sprintf("%s %s [%s] return %s",
			rec.Type.MarkString(), rec.Time, rec.IDString(),
			rec.Args)
	default:
		switch rec.Type {
		case beginRecord:
			return fmt.Sprintf("%s %s [%s] %s: %s",
				rec.Type.MarkString(), rec.Time, rec.IDString(),
				rec.Name, rec.Args)
		default:
			return fmt.Sprintf("%s %s [%s] %s",
				rec.Type.MarkString(), rec.Time, rec.IDString(),
				rec.Args)
		}
	}
}
