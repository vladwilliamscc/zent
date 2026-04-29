// Copyright (c) 2013-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"io"
	"reflect"
	"testing"
)

func TestPing(t *testing.T) {
	pver := ProtocolVersion
	msg := NewMsgPing(123123, 456)

	if msg.Nonce != 123123 {
		t.Fatalf("NewMsgPing: nonce = %d, want %d", msg.Nonce, uint64(123123))
	}
	if msg.Height != 456 {
		t.Fatalf("NewMsgPing: height = %d, want %d", msg.Height, int32(456))
	}
	if cmd := msg.Command(); cmd != "ping" {
		t.Fatalf("NewMsgPing: command = %q, want %q", cmd, "ping")
	}
	if maxPayload := msg.MaxPayloadLength(pver); maxPayload != 12 {
		t.Fatalf("MaxPayloadLength: got %d, want %d", maxPayload, uint32(12))
	}
}

func TestPingWire(t *testing.T) {
	tests := []struct {
		in   MsgPing
		out  MsgPing
		buf  []byte
		pver uint32
		enc  MessageEncoding
	}{
		{
			in:   MsgPing{Nonce: 123123, Height: 456},
			out:  MsgPing{Nonce: 123123, Height: 456},
			buf:  []byte{0xf3, 0xe0, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0xc8, 0x01, 0x00, 0x00},
			pver: ProtocolVersion,
			enc:  BaseEncoding,
		},
		{
			in:   MsgPing{Nonce: 456456, Height: -7},
			out:  MsgPing{Nonce: 456456, Height: -7},
			buf:  []byte{0x08, 0xf7, 0x06, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf9, 0xff, 0xff, 0xff},
			pver: BIP0031Version,
			enc:  BaseEncoding,
		},
	}

	for i, test := range tests {
		var buf bytes.Buffer
		if err := test.in.OmcEncode(&buf, test.pver, test.enc); err != nil {
			t.Fatalf("OmcEncode #%d error: %v", i, err)
		}
		if !bytes.Equal(buf.Bytes(), test.buf) {
			t.Fatalf("OmcEncode #%d got %x, want %x", i, buf.Bytes(), test.buf)
		}

		var msg MsgPing
		if err := msg.OmcDecode(bytes.NewReader(test.buf), test.pver, test.enc); err != nil {
			t.Fatalf("OmcDecode #%d error: %v", i, err)
		}
		if !reflect.DeepEqual(msg, test.out) {
			t.Fatalf("OmcDecode #%d got %#v, want %#v", i, msg, test.out)
		}
	}
}

func TestPingWireErrors(t *testing.T) {
	msg := MsgPing{Nonce: 123123, Height: 456}
	shortPayload := []byte{0xf3, 0xe0, 0x01, 0x00, 0x00, 0x00}

	w := newFixedWriter(2)
	if err := msg.OmcEncode(w, ProtocolVersion, BaseEncoding); err != io.ErrShortWrite {
		t.Fatalf("OmcEncode wrong error got %v, want %v", err, io.ErrShortWrite)
	}

	var decoded MsgPing
	if err := decoded.OmcDecode(bytes.NewReader(shortPayload), ProtocolVersion, BaseEncoding); err != io.ErrUnexpectedEOF {
		t.Fatalf("OmcDecode wrong error got %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

type fixedWriter int

func newFixedWriter(max int) io.Writer {
	writer := fixedWriter(max)
	return &writer
}

func (w *fixedWriter) Write(p []byte) (int, error) {
	max := int(*w)
	if len(p) > max {
		return 0, io.ErrShortWrite
	}
	*w -= fixedWriter(len(p))
	return len(p), nil
}
