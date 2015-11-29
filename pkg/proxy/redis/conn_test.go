// Copyright 2014 Wandoujia Inc. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package redis

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/wandoulabs/codis/pkg/utils/assert"
	"github.com/wandoulabs/codis/pkg/utils/atomic2"
	"github.com/wandoulabs/codis/pkg/utils/errors"
)

var (
	connPool	*sync.Pool
)

func init() {
	connPool = &sync.Pool{
		New: func() interface{} {
			return nil
		},
	} 
}

func newConnPair() (*Conn, *Conn) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.MustNoError(err)
	defer l.Close()

	const bufsize = 128 * 1024

	cc := make(chan *Conn, 1)
	go func() {
		defer close(cc)
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			x, ok := connPool.Get().(*Conn)
			if !ok || x == nil {
				cc <- NewConnSize(c, bufsize)
			} else {
				x.Reset(c)
				cc <- x
			}
		}
	}()

	conn1, err := DialTimeout(l.Addr().String(), bufsize, time.Millisecond*50)
	assert.MustNoError(err)

	conn2, ok := <-cc
	assert.Must(ok)
	return conn1, conn2
}

func TestConnReaderTimeout(t *testing.T) {
	resp := NewString([]byte("hello world"))

	conn1, conn2 := newConnPair()
	defer func() {
		connPool.Put(conn2)
	}()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		var err error

		conn1.ReaderTimeout = time.Millisecond * 10
		_, err = conn1.Reader.Decode()
		assert.Must(err != nil && IsTimeout(err))

		conn1.Reader.Err = nil
		conn1.ReaderTimeout = 0
		_, err = conn1.Reader.Decode()
		assert.MustNoError(err)

		_, err = conn1.Reader.Decode()
		assert.Must(err != nil && errors.Equal(err, io.EOF))
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		var err error

		time.Sleep(time.Millisecond * 100)

		err = conn2.Writer.Encode(resp, true)
		assert.MustNoError(err)

		conn2.Close()
	}()

	wg.Wait()

	conn1.Close()
	conn2.Close()
}

func TestConnWriterTimeout(t *testing.T) {
	resp := NewString([]byte("hello world"))

	conn1, conn2 := newConnPair()
	defer func() {
		connPool.Put(conn2)
	}()

	var wg sync.WaitGroup

	var count atomic2.Int64

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer conn2.Close()

		conn2.WriterTimeout = time.Millisecond * 50
		for {
			if err := conn2.Writer.Encode(resp, true); err != nil {
				assert.Must(IsTimeout(err))
				return
			}
			count.Incr()
		}
	}()

	wg.Wait()

	for i := count.Get(); i != 0; i-- {
		_, err := conn1.Reader.Decode()
		assert.MustNoError(err)
	}
	_, err := conn1.Reader.Decode()
	assert.Must(err != nil && errors.Equal(err, io.EOF))

	conn1.Close()
	conn2.Close()
}
