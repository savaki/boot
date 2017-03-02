package loggly

var (
	pool = make(chan *block, 1024)
)

type Option func(*Client)

type block struct {
	buffer     []byte
	bufferSize int
	poolable   bool
	offset     int
}

func (b *block) HasCapacity(data []byte) bool {
	return b.offset+len(data) < b.bufferSize
}

func (b *block) Append(data []byte) {
	if data == nil {
		return
	}

	for _, d := range data {
		b.buffer[b.offset] = d
		b.offset++
	}
}

func (b *block) Bytes() []byte {
	return b.buffer[0:b.offset]
}

func (b *block) Release() {
	if b.poolable {
		select {
		case pool <- b:
		default:
		}
	}
}

func newBlock(defaultSize, size int) *block {
	var b *block

	if size > defaultSize {
		return &block{
			buffer:     make([]byte, size),
			bufferSize: size,
		}
	}

	select {
	case v := <-pool:
		b = v
	default:
		b = &block{
			buffer:     make([]byte, defaultSize),
			bufferSize: defaultSize,
			poolable:   true,
		}
	}

	b.offset = 0
	return b
}
