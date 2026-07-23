package productsession

import (
	"errors"
	"io"
	"net"
	"os"
)

// Relay carries the raw app-server proxy stream after the framed supervisor
// response. A supervisor-side EOF is terminal even while sshd still holds its
// input pipe open; waiting for that copier would turn a clean service stop into
// a client timeout.
func Relay(connection *net.UnixConn, input io.Reader, output io.Writer) error {
	type copyResult struct {
		input bool
		err   error
	}
	results := make(chan copyResult, 2)
	go func() {
		// Hide ReaderFrom/WriterTo so io.Copy cannot select Linux splice for
		// the SSH pipes. A splice waiter can survive peer teardown even after
		// the supervisor has closed and drained its Unix connection.
		_, err := io.Copy(
			writerOnly{Writer: connection},
			readerOnly{Reader: input},
		)
		_ = connection.CloseWrite()
		results <- copyResult{input: true, err: err}
	}()
	go func() {
		_, err := io.Copy(
			writerOnly{Writer: output},
			readerOnly{Reader: connection},
		)
		results <- copyResult{err: err}
	}()

	first := <-results
	if !first.input {
		_ = connection.Close()
		if closer, ok := input.(io.Closer); ok {
			_ = closer.Close()
		}
		return relayCopyError(first.err)
	}

	second := <-results
	_ = connection.Close()
	if closer, ok := input.(io.Closer); ok {
		_ = closer.Close()
	}
	return errors.Join(relayCopyError(first.err), relayCopyError(second.err))
}

type readerOnly struct {
	io.Reader
}

type writerOnly struct {
	io.Writer
}

func relayCopyError(err error) error {
	if err != nil &&
		!errors.Is(err, net.ErrClosed) &&
		!errors.Is(err, os.ErrClosed) &&
		!errors.Is(err, io.ErrClosedPipe) {
		return err
	}
	return nil
}
