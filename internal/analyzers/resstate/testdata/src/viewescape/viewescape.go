package viewescape

import (
	"bufio"
	"bytes"
)

func viewescapeReported(sc *bufio.Scanner, lines [][]byte) [][]byte {
	for sc.Scan() {
		lines = append(lines, sc.Bytes()) // want "points into the buffer of"
	}
	return lines
}

func viewescapeReportedSend(sc *bufio.Scanner, ch chan []byte) {
	for sc.Scan() {
		ch <- sc.Bytes() // want "points into the buffer of"
	}
}

type viewHolder struct {
	b []byte
}

func viewescapeReportedField(sc *bufio.Scanner, h *viewHolder) {
	for sc.Scan() {
		h.b = sc.Bytes() // want "points into the buffer of"
	}
}

func viewescapeReportedMap(sc *bufio.Scanner, m map[int][]byte, k int) {
	for sc.Scan() {
		m[k] = sc.Bytes() // want "points into the buffer of"
	}
}

func viewescapeReportedOutside(sc *bufio.Scanner) []byte {
	var out []byte
	for sc.Scan() {
		out = sc.Bytes() // want "points into the buffer of"
	}
	return out
}

func viewescapeReportedReturn(sc *bufio.Scanner) []byte {
	for sc.Scan() {
		return sc.Bytes() // want "points into the buffer of"
	}
	return nil
}

func viewescapeReportedVar(sc *bufio.Scanner, lines [][]byte) [][]byte {
	for sc.Scan() {
		v := sc.Bytes()
		lines = append(lines, v) // want "points into the buffer of"
	}
	return lines
}

func viewescapeReportedPeek(r *bufio.Reader, out [][]byte) [][]byte {
	for {
		p, _ := r.Peek(1)
		out = append(out, p) // want "points into the buffer of"
		_, _ = r.Discard(1)
		if len(p) == 0 {
			break
		}
	}
	return out
}

func viewescapeReportedBuffer(buf *bytes.Buffer, out [][]byte) [][]byte {
	for buf.Len() > 0 {
		out = append(out, buf.Bytes()) // want "points into the buffer of"
		_, _ = buf.Write([]byte("x"))
	}
	return out
}

func viewescapeSilent(sc *bufio.Scanner, lines [][]byte, names []string) ([]string, [][]byte) {
	for sc.Scan() {
		lines = append(lines, bytes.Clone(sc.Bytes()))
	}
	for sc.Scan() {
		names = append(names, string(sc.Bytes()))
	}
	return names, lines
}

func viewescapeSilentSpread(sc *bufio.Scanner, buf []byte) []byte {
	for sc.Scan() {
		buf = append(buf, sc.Bytes()...)
	}
	return buf
}

func viewescapeSilentLoopLocal(sc *bufio.Scanner) int {
	n := 0
	for sc.Scan() {
		var b []byte
		b = sc.Bytes()
		n += len(b)
	}
	return n
}

func viewescapeSilentLoopArray(sc *bufio.Scanner) int {
	n := 0
	for sc.Scan() {
		var row [1][]byte
		row[0] = sc.Bytes()
		n += len(row[0])
	}
	return n
}

func viewescapeSilentReassigned(sc *bufio.Scanner) [][]byte {
	var keep [][]byte
	for sc.Scan() {
		b := sc.Bytes()
		b = bytes.Clone(b)
		keep = append(keep, b)
	}
	return keep
}

func viewescapeReportedNested(sc *bufio.Scanner) [][]byte {
	var lines [][]byte
	for sc.Scan() {
		for sc.Scan() {
			lines = append(lines, sc.Bytes()) // want "points into the buffer of"
		}
	}
	return lines
}

func viewescapeSilentPostOnly(buf *bytes.Buffer, out [][]byte) [][]byte {
	for i := 0; i < 2; _, _ = buf.Write(nil) {
		out = append(out, buf.Bytes())
		i++
	}
	return out
}

func viewescapeReportedPreLoop(sc *bufio.Scanner) [][]byte {
	var out [][]byte
	line := sc.Bytes()
	for sc.Scan() {
		out = append(out, line) // want "points into the buffer of"
	}
	return out
}

func viewescapeReportedMultiTarget(r *bufio.Reader, h *viewHolder) {
	for i := 0; i < 2; i++ {
		h.b, _ = r.Peek(1) // want "points into the buffer of"
		_, _ = r.Discard(1)
	}
}

func viewescapeReportedBufferRead(buf *bytes.Buffer, out [][]byte) [][]byte {
	for buf.Len() > 0 {
		out = append(out, buf.Bytes()) // want "points into the buffer of"
		_, _ = buf.Read(make([]byte, 1))
	}
	return out
}

func viewescapeReportedReaderReset(r *bufio.Reader, rd *bytes.Buffer) [][]byte {
	var out [][]byte
	p, _ := r.Peek(1)
	for i := 0; i < 2; i++ {
		out = append(out, p) // want "points into the buffer of"
		r.Reset(rd)
	}
	return out
}

func viewescapeSilentSelfInvalid(b *bufio.Reader) []byte {
	var frag []byte
	for {
		var e error
		frag, e = b.ReadSlice('\n')
		if e == nil {
			break
		}
		if e != bufio.ErrBufferFull {
			break
		}
	}
	return frag
}

type viewConn struct{ *bufio.Scanner }

func (viewConn) Write(p []byte) (int, error) { return len(p), nil }

func viewescapeSilentForeignInvalidator(c viewConn, out [][]byte) [][]byte {
	for _, s := range []string{"a"} {
		_ = s
		out = append(out, c.Bytes())
		_, _ = c.Write(nil)
	}
	return out
}

func viewescapeSilentRangeReassign(sc *bufio.Scanner, ch chan []byte, out [][]byte) [][]byte {
	for sc.Scan() {
		b := sc.Bytes()
		for b = range ch {
		}
		out = append(out, b)
	}
	return out
}
