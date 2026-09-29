package bashrun

// Command output is a bounded tail. Draining continues after the cap, so a
// verbose child cannot deadlock on its stdout pipe or exhaust host memory.
const outputBytes = 1 << 20

type outputBuffer struct {
	data  []byte
	total int64
}

func (b *outputBuffer) Write(chunk []byte) (int, error) {
	n := len(chunk)
	b.total += int64(n)
	if n >= outputBytes {
		b.data = append(b.data[:0], chunk[n-outputBytes:]...)
	} else {
		if excess := len(b.data) + n - outputBytes; excess > 0 {
			copy(b.data, b.data[excess:])
			b.data = b.data[:len(b.data)-excess]
		}
		b.data = append(b.data, chunk...)
	}
	return n, nil
}

func (b *outputBuffer) String() string  { return string(b.data) }
func (b *outputBuffer) truncated() bool { return b.total > int64(len(b.data)) }
