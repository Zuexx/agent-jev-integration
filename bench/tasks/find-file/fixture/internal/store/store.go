package store

import "example.com/ff/internal/codec"

func Put(b []byte) uint32 { return codec.ComputeChecksum(b) }
