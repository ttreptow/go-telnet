package telnet

import (
	"bufio"
	"compress/zlib"
	"errors"
	"io"
)

const (
	IAC = 255

	GA = 249
	SB = 250
	SE = 240

	WILL = 251
	WONT = 252
	DO   = 253
	DONT = 254

	OPT_COMPRESS2 = 86
)

var (
	errCorrupted = errors.New("Corrupted")
)

// An internalDataReader deals with "un-escaping" according to the TELNET protocol.
//
// In the TELNET protocol byte value 255 is special.
//
// The TELNET protocol calls byte value 255: "IAC". Which is short for "interpret as command".
//
// The TELNET protocol also has a distinction between 'data' and 'commands'.
//
// (DataReader is targetted toward TELNET 'data', not TELNET 'commands'.)
//
// If a byte with value 255 (=IAC) appears in the data, then it must be escaped.
//
// Escaping byte value 255 (=IAC) in the data is done by putting 2 of them in a row.
//
// So, for example:
//
//	[]byte{255} -> []byte{255, 255}
//
// Or, for a more complete example, if we started with the following:
//
//	[]byte{1, 55, 2, 155, 3, 255, 4, 40, 255, 30, 20}
//
// ... TELNET escaping would produce the following:
//
//	[]byte{1, 55, 2, 155, 3, 255, 255, 4, 40, 255, 255, 30, 20}
//
// (Notice that each "255" in the original byte array became 2 "255"s in a row.)
//
// DataReader deals with "un-escaping". In other words, it un-does what was shown
// in the examples.
//
// So, for example, it does this:
//
//	[]byte{255, 255} -> []byte{255}
//
// And, for example, goes from this:
//
//	[]byte{1, 55, 2, 155, 3, 255, 255, 4, 40, 255, 255, 30, 20}
//
// ... to this:
//
//	[]byte{1, 55, 2, 155, 3, 255, 4, 40, 255, 30, 20}
type internalDataReader struct {
	wrapped  io.ReadWriter
	buffered *bufio.Reader
}

// newDataReader creates a new DataReader reading from 'r'.
func newDataReader(rw io.ReadWriter) *internalDataReader {
	buffered := bufio.NewReader(rw)

	reader := internalDataReader{
		wrapped:  rw,
		buffered: buffered,
	}

	return &reader
}

// Read reads the TELNET escaped data from the  wrapped io.Reader, and "un-escapes" it into 'data'.
func (r *internalDataReader) Read(data []byte) (n int, err error) {
	p := data

	for len(p) > 0 {
		var b byte

		if n > 0 && r.buffered.Buffered() < 1 {
			break
		}

		b, err = r.buffered.ReadByte()
		if nil != err {
			return n, err
		}

		if IAC == b {
			var peeked []byte

			peeked, err = r.buffered.Peek(1)
			if nil != err {
				return n, err
			}

			switch peeked[0] {
			case DO, DONT:
				_, err = r.buffered.Discard(2)
				if nil != err {
					return n, err
				}
			case WILL, WONT:
				_, err = r.buffered.Discard(1)
				if nil != err {
					return n, err
				}
				opt, err := r.buffered.Peek(1)
				if nil != err {
					return n, err
				}
				err = r.handleOption(opt[0])
				if nil != err {
					return n, err
				}
				_, err = r.buffered.Discard(1)
				if nil != err {
					return n, err
				}
			case IAC:
				p[0] = IAC
				n++
				p = p[1:]

				_, err = r.buffered.Discard(1)
				if nil != err {
					return n, err
				}
			case SB:
				for {
					var b2 byte
					b2, err = r.buffered.ReadByte()
					if nil != err {
						return n, err
					}

					switch b2 {
					case IAC:
						peeked, err = r.buffered.Peek(1)
						if nil != err {
							return n, err
						}

						if IAC == peeked[0] {
							_, err = r.buffered.Discard(1)
							if nil != err {
								return n, err
							}
						}

						if SE == peeked[0] {
							_, err = r.buffered.Discard(1)
							if nil != err {
								return n, err
							}
							break
						}
					case OPT_COMPRESS2:
						//IAC
						_, err = r.buffered.ReadByte()
						if nil != err {
							return n, err
						}
						//SE
						_, err = r.buffered.ReadByte()
						if nil != err {
							return n, err
						}
						zReader, err := zlib.NewReader(r.wrapped)
						if nil != err {
							return n, err
						}
						r.buffered = bufio.NewReader(zReader)
						break
					}
				}
			case SE:
				_, err = r.buffered.Discard(1)
				if nil != err {
					return n, err
				}
			case GA:
				_, err = r.buffered.Discard(1)
				if nil != err {
					return n, err
				}
				break
			default:
				// If we get in here, this is not following the TELNET protocol.
				//@TODO: Make a better error.
				err = errCorrupted
				return n, err
			}
		} else {

			p[0] = b
			n++
			p = p[1:]
		}
	}

	return n, nil
}

func (r *internalDataReader) handleOption(opt byte) error {
	switch opt {
	case OPT_COMPRESS2:
		_, err := r.wrapped.Write([]byte{IAC, DO, opt})
		if err != nil {
			return err
		}
	default:
		_, err := r.wrapped.Write([]byte{IAC, DONT, opt})
		if err != nil {
			return err
		}
	}
	return nil
}
