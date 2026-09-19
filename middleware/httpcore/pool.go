// This file holds the body buffer pool of one Core. The pool lives on the Core, because
// a package-level pool would be mutable package state, which this project forbids.
package httpcore

import "sync"

// bodyPool hands one buffer to each request that captures a body, so a small body does
// not allocate the whole cap every time.
type bodyPool struct {
	pool sync.Pool
	size int
}

// newBodyPool builds the pool of one Core, with buffers of the given size.
func newBodyPool(size int) *bodyPool {
	p := &bodyPool{size: size}
	p.pool.New = func() any {
		buf := make([]byte, size)
		return &buf
	}
	return p
}

// get returns a buffer of the pool's size.
func (p *bodyPool) get() []byte {
	buf, _ := p.pool.Get().(*[]byte)
	if buf == nil || cap(*buf) < p.size {
		return make([]byte, p.size)
	}
	return (*buf)[:p.size]
}

// put returns a buffer to the pool. A buffer that is not the pool's size is dropped,
// because a later reader needs the whole size.
func (p *bodyPool) put(buf []byte) {
	if cap(buf) < p.size {
		return
	}
	full := buf[:cap(buf)]
	p.pool.Put(&full)
}
