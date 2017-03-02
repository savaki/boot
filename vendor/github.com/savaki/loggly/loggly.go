package loggly

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	Endpoint = "http://logs-01.loggly.com/bulk/{token}/tag/bulk/"
)

type request struct {
	data []byte
	n    int
	err  error
}

type Client struct {
	sync.Mutex
	cancel      func()
	ctx         context.Context
	endpoint    string
	bufferSize  int
	interval    time.Duration
	threshold   int
	publishFunc func([]byte) error
	ch          chan *block
	flush       chan chan error
}

func New(token string, options ...Option) *Client {
	endpoint := strings.Replace(Endpoint, "{token}", token, -1)

	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{
		cancel:     cancel,
		ctx:        ctx,
		bufferSize: 4 * 1024,
		ch:         make(chan *block, 256),
		flush:      make(chan chan error, 16),
		interval:   time.Second * 3,
		threshold:  1024 * 1024,
		publishFunc: func(data []byte) error {
			resp, err := http.Post(endpoint, "text/plain", bytes.NewReader(data))
			if resp != nil {
				defer resp.Body.Close()
			}
			return err
		},
	}

	for _, opt := range options {
		opt(client)
	}

	go client.Start()

	return client
}

func (c *Client) Close() {
	c.cancel()
}

func (c *Client) Flush() error {
	ch := make(chan error)
	defer close(ch)
	c.flush <- ch // send the request
	return <-ch   // wait for a reply
}

func (c *Client) Write(data []byte) (int, error) {
	c.Lock()
	defer c.Unlock()

	block := newBlock(c.bufferSize, len(data))
	block.Append(data)

	select {
	case c.ch <- block:
	default:
	}

	return len(data), nil
}

func (c *Client) Start() {
	buffer := newBlock(c.bufferSize, 1024*1024*2)

	publish := func() {
		if buffer.offset > 0 {
			c.publishFunc(buffer.Bytes())
			buffer.offset = 0
		}
	}

	timer := time.NewTimer(c.interval)
	for {
		timer.Reset(c.interval)

		select {
		case <-c.ctx.Done():
			return
		case <-timer.C:
			publish()

		case v := <-c.flush:
			publish()
			v <- nil

		case b := <-c.ch:
			buffer.Append(b.Bytes())
			if buffer.offset > c.threshold {
				publish()
			}
			b.Release()
		}
	}
}

func BufferSize(bufferSize int) Option {
	return func(c *Client) {
		c.bufferSize = bufferSize
	}
}

func Interval(d time.Duration) Option {
	return func(c *Client) {
		c.interval = d
	}
}

func Publish(fn func([]byte) error) Option {
	return func(c *Client) {
		c.publishFunc = fn
	}
}

func Threshold(n int) Option {
	return func(c *Client) {
		c.threshold = n
	}
}
