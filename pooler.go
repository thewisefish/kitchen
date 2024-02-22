package kitchen

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
)

//go:generate $GOPATH/bin/stringer -type=poolerErrorCode -linecomment -output pooler_generated.go
type poolerErrorCode int

const (
	nilPoolable         poolerErrorCode = iota // nil Poolable
	nilCreatorFunc                         // nil New function passed to CreatePooler
	nilAfterGetFunc                        // nil AfterGet function passed to CreatePooler
	nilPoolerToRecycler                    // nil Pooler passed to CreateRecycler
	poolerDisabled                         // Pooler disabled
	recyclerDisabled                       // Recycler disabled
	initpoolerError                        // error creating Recycler
)

func (e poolerErrorCode) Error() error { return errors.New(e.String()) }

var (
	NilPoolable  = nilPoolable.Error()
	NilCreatorFunc  = nilCreatorFunc.Error()
	NilAfterGetFunc  = nilAfterGetFunc.Error()
	NilPoolerToRecycler = nilPoolerToRecycler.Error()
	PoolerDisabled = poolerDisabled.Error()
	RecyclerDisabled = recyclerDisabled.Error()
	InitPoolerError = initpoolerError.Error()
)
func AfterGetNoop[T any](x T) T    { return x }

/*
	Poolable defines a generic struct used to initialize instances of wrappedPool.

New is the function passed to sync.Pool as the value for New() during its creation.

AfterGet is the function called after calls to pool.Get to reset/zero out the retrieved value to a defined default state. This is to mitigate the issues that can arise from reusing a populated/"dirty" value. If you are sure that such issues aren't relevant to the values you're using the pool for, you can set a no-op function such as AfterGetNoop[T]. AfterGet should handle nil values if your New function can create them.

CreatePool requires that functions be defined for New and AfterGet. While a sync.Pool will simply return nil if its New() property is not set and there's no free objects in its cache, instances of Pooler assume that any nil values returned by Get calls to its sync.Pool were created intentionally.

BeforePut is an optional function that, if set, is called on values before a Pooler returns that value to its pool using sync.Pool.Put. Set BeforePut for cases where references need to be cleared fron the value before being returned to the pool, such as pointers to other data structures. As an example, the pool used for instances of recyclable[T] (the underlying struct for Wrapped[T]) has a BeforePut handler defined which resets its value field to nil.

Size is an optional value that, if set, define the size of the Pooler's underlying channel.
*/
type Poolable[T any] struct {
	sync.RWMutex `json:"-"`
	New          func() T  `json:"-"`
	AfterGet     func(T) T `json:"-"`
	BeforePut    func(T)   `json:"-"`
	Size         int       `json:"size"`
}

// Returns an error corresponding to any issues found, or nil if there are no issues
func (r *Poolable[T]) Validate() error {
	if r == nil {
		return NilPoolable
	}
	r.RLock()
	defer r.RUnlock()
	if r.New == nil {
		return NilCreatorFunc
	}
	if r.AfterGet == nil {
		return NilAfterGetFunc
	}
	return nil
}

/*
CreatePool[T] and Poolable[T].CreatePool create a Pooler from a collection of functions or a defined Poolable[T]. The two CreatePool functions/methods are similar.
*/
func CreatePool[T any](New func() T, afterGet func(T) T, beforePut func(T), size int) (Pooler[T], error) {
	if New == nil {
		return nil, NilCreatorFunc
	}
	if afterGet == nil {
		return nil, NilAfterGetFunc
	}
	var dump chan *T
	if size > 0 {
		dump = make(chan *T, size)
	}
	if dump == nil {
		dump = make(chan *T)
	}
	nc := runtime.NumCPU() //if there exists a bug/situation that makes this 0, make 1
	if nc < 1 {
		nc = 1
	}
	rv := &wrappedPool[T]{
		reference: &Poolable[T]{
			New:       New,
			AfterGet:  afterGet,
			BeforePut: beforePut,
			Size:      size,
		},
		dump:      dump,
		finished:  make(chan struct{}),
		listeners: nc,
	}
	rv.pool = sync.Pool{New: func() any { return rv.reference.New() }}
	rv.monitor()
	rv.live.Store(true)
	return rv, nil
}

/* Poolable[T].CreatePool copies the properties from the calling Poolable[T] in case the receiver is edited later on. This means you can declare your pool configuration somewhere as a Poolable[T] and use the same one to generate new Pooler[T] instances. This also lets you change the configuration between Pooler creations without affecting preexisting ones. */

func (r *Poolable[T]) CreatePool() (Pooler[T], error) {
	if r == nil {
		return nil, NilPoolable
	}
	r.RLock()
	defer r.RUnlock()
	err := r.Validate()
	if err != nil {
		return nil, err
	}
	var dump chan *T
	if r.Size > 0 {
		dump = make(chan *T, r.Size)
	}
	if dump == nil {
		dump = make(chan *T)
	}
	nc := runtime.NumCPU()
	if nc < 1 {
		nc = 1
	}
	rv := &wrappedPool[T]{
		reference: &Poolable[T]{
			New:       r.New,
			AfterGet:  r.AfterGet,
			BeforePut: r.BeforePut,
			Size:      r.Size,
		},
		dump:      dump,
		finished:  make(chan struct{}),
		listeners: nc,
	}
	rv.pool = sync.Pool{New: func() any { return rv.reference.New() }}
	rv.monitor()
	rv.live.Store(true)
	return rv, nil
}

/* wrappedPool[T] wraps a sync.Pool and ensures that only values of type T are retrieved from, or added to it. A copy of the Poolable[T] used to instantiate it is stored since internally many of its methods just directly call the functions defined on that reference *Poolable[T]. */

type wrappedPool[T any] struct {
	pool      sync.Pool
	dump      chan (*T)    `json:"-"`
	reference *Poolable[T] `json:"-"` //r.reference copied and made unexported to prevent needing to actually use the mutex on r.reference
	mgr       sync.Once
	finished  chan (struct{})
	live      atomic.Bool
	listeners int // runtime.NumCPU, no lower than 1
}

// type asserter for values retrieved from the sync.Pool. Attempts to create a new instance if the type check somehow fails, such as for a nil value.
func (r *wrappedPool[T]) check(x any) T {
	var rv T
	v, ok := x.(T)
	if !ok {
		rv = r.reference.New()
	}
	if ok {
		rv = v
	}
	rs := r.reference.AfterGet(rv)
	return rs
}

// Wrapper for sync.Pool.Get. Attempts type assertion before returning the retrieved value.
func (r *wrappedPool[T]) Get() T {
	v := r.pool.Get()
	rv := r.check(v)
	return rv
}

// Type-constrained wrapper for sync.Pool.Put. nil x is fine since sync.Pool ignores it
func (r *wrappedPool[T]) Put(x T) {
	if r.reference.BeforePut != nil {
		r.reference.BeforePut(x)
	}
	r.pool.Put(x)
}

// Sends a value back to the Pool through a channel if the pool is still live. Note that Toss() takes a pointer to T.
func (r *wrappedPool[T]) Toss(x *T) {
	if r.live.Load() && x != nil { //check to avoid the situation where this causes a deadlock due to the Pooler being disabled, therefore no longer listening
		if len(r.dump) < r.reference.Size {
			r.dump <- x
			return
		}
		r.Put(*x)
	}
}

// Returns itself. Defined to allow a Pooler[T] interface to have a reference to its underlying struct. Necessary to circumvent instantiation cycle issues in the implementation of Recycler[T]
func (r *wrappedPool[T]) source() *wrappedPool[T] { return r }

// defined to expose pool liveness to recycler New
func (r *wrappedPool[T]) enabled() bool {
	//if r==nil {return false}
	ok := r.live.Load()
	return ok
}

// wrapper/resetter for the sync.Once that executes the functions that regulate the pool's life cycle
func (r *wrappedPool[T]) manage(f func(), reset bool) {
	if r.live.Load() {
		r.mgr.Do(f)
		if reset {
			r.mgr = sync.Once{}
		}
	}
}

func (r *wrappedPool[T]) disable() {
	r.manage(func() {
		ok := r.live.Swap(false) //forces anything pending a load check eg .enabled() to go first -- one of several check to prevent deadlock
		if ok {
			for i := 0; i < r.listeners; i++ {
				r.finished <- struct{}{}
			}
		}
	}, false)
}
func (r *wrappedPool[T]) monitor() {
	for d := 0; d < r.listeners; d++ {
		go r.clearDump()
	}
}

// Called on pool creation. Waits on the channel for values to return to the pool. Returns when disable() is called
func (r *wrappedPool[T]) clearDump() {
	for {
		select {
		case m := <-r.dump:
			if m != nil {
				r.Put(*m)
			}
		case _ = <-r.finished:
			return
		}
	}
}

/* Type recyclable[T] defines a struct that holds a pointer to a value T. This is the struct used under the hood to wrap values made with a Recycler[T]. Its purpose is to implement Wrapped[T]. */
type recyclable[T any] struct {
	val    atomic.Pointer[T]
	finish func(*recyclable[T])
}

// sets the value of wrapped pointer, or nil to clear. Defined to expose to Wrapped[T] interface
func (r *recyclable[T]) set(x *T) { r.val.Store(x) }

// Returns the wrapped value
func (r *recyclable[T]) Value() *T {
	rv := r.val.Load()
	return rv
}

/* Discard sends recyclable[T] back to the wrappedPool[T] that made it, to be returned it and its wrapped value to their pools. This signals the end of the object's current lifetime, and as such should be called when it is no longer needed. Do not attempt to use instances of recyclable[T] or Wrapped[T] after calling Discard. Your program will likely panic. */
func (r *recyclable[T]) Discard() { r.finish(r) }

/* type recyclingPlant[T] defines a struct that contains a reference to a wrappedPool of type T along with the wrappedPool created to store the corresponding Wrapped[T] for T. */
type recyclingPlant[V Wrapped[T], T any] struct {
	pool    Pooler[T]
	wrapper Pooler[V]
}

func (r *recyclingPlant[V, T]) Make() Wrapped[T] {
//	if r.wrapper.enabled() {
	if r.pool != nil && r.wrapper != nil {
		wp := r.wrapper.Get()
		if r.pool.enabled() {
			val := r.pool.Get()
			wp.set(&val)
		}
		return wp
	}
	return nil
}
func (r *recyclingPlant[V, T]) disable() {
	Destroy(r.wrapper)
}

/* Interface definitions. With the exception of Poolable[T], none of the structs in this package are directly exported or interactable. */

// interface for wrappedPool[T].
type Pooler[T any] interface {
	Get() T
	Put(T)
	Toss(*T)
	source() *wrappedPool[T]
	manage(func(), bool)
	disable()
	enabled() bool
}

// placeholder New generator
func makeRecyclable[T any]() *recyclable[T] { return &recyclable[T]{} }

// interface for recyclingPlant[T].
type Recycler[T any] interface {
	Make() Wrapped[T] //makes pooled objects
	disable()
}

func initRecycler[V *recyclable[T], T any](r Pooler[T]) (Recycler[T], error) {
	if r == nil {
		return nil, NilPoolerToRecycler
	}
	maker := makeRecyclable[T]
	bp := func(x *recyclable[T]) {
		if x == nil {
			return
		}
		xv := x.val.Swap(nil)
		r.Toss(xv) //Toss has a liveness check and can potentially not block
	}
	rp, err := CreatePool[*recyclable[T]](maker, AfterGetNoop[*recyclable[T]], bp, 0)
	if err!=nil {
		Destroy(rp)
		return nil, err
	}
	//redefine afterGet logic to set .finish property on created recyclables
	//rp.source().reference.Lock() //nothing else contends with this lock
	definer := func() *recyclable[T] {
		x := maker()
		x.finish = rp.Put
		return x
	}
	rp.source().reference.New = definer
	//rp.source().reference.Unlock()
	rv := &recyclingPlant[*recyclable[T], T]{pool: r, wrapper: rp}
	if !r.enabled() { //check last to minimize ABA problem shenanigans -- return recyclingPlant at this point since the recycler will get Destroy()'d anyway
		err = RecyclerDisabled
	}
	return rv, err
}

/* CreateRecycler[T] will return nil if an invalid Pooler[T] is passed */
func CreateRecycler[T any](p Pooler[T]) (Recycler[T], error) {
	rp, err := initRecycler[*recyclable[T], T](p)
	if err != nil {
		Destroy(rp) //has its own nil check for nil p, shuts down recyclers bound to bugged Poolers
		return nil, err
	}
	return rp, nil
}

// interface for recyclable[T]. Wrapper for values wrapped by Recycler[T]
type Wrapped[T any] interface {
	Value() *T
	Discard()
	set(*T)
}

// Destroyable is the interface for destroying both pools and their recyclers.
type Destroyable interface {
	disable()
}

// two functions required for type inference reasons
func Destroy(p Destroyable) {
	if p != nil {
		p.disable()
	}
}
