package main

import (
	"bufio"
	"bytes"
	"io"

	"github.com/emersion/go-message/textproto"
)

// Set only the synthetic address headers; preserve all other headers and the
// multipart body. Header parsing handles the writer's folded lines and casing.
func selftestEncodedAddressNames(raw []byte) ([]byte, error) {
	reader := bufio.NewReader(bytes.NewReader(raw))
	header, err := textproto.ReadHeader(reader)
	if err != nil {
		return nil, err
	}
	header.Set("From", `"=?utf-8?B?5Y2P6K6u5rWL6K+V?=" <sender@example.invalid>`)
	header.Set("To", `"=?utf-8?B?5pS25Lu25rWL6K+V?=" <demo@example.invalid>`)
	var result bytes.Buffer
	if err := textproto.WriteHeader(&result, header); err != nil {
		return nil, err
	}
	if _, err := io.Copy(&result, reader); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}
