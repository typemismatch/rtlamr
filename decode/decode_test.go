package decode

import (
	"math"
	"testing"
)

func NewPacketConfig(symbolLength int) (cfg PacketConfig) {
	cfg.CenterFreq = 912600155
	cfg.DataRate = 32768
	cfg.SymbolLength = symbolLength
	cfg.PreambleSymbols = 21
	cfg.PacketSymbols = 96

	cfg.Preamble = "111110010101001100000"

	return
}

func TestMagLUT(t *testing.T) {
	lut := NewMagLUT()

	var input []byte
	for i := 0; i < 0x100; i++ {
		for j := i; j < 0x100; j++ {
			input = append(input, byte(i), byte(j))
		}
	}

	t.Logf("Input: %d\n", input)

	output := make([]float64, len(input)>>1)
	lut.Execute(input, output)

	min := math.MaxFloat64
	max := -math.MaxFloat64

	for _, val := range output {
		if min > val {
			min = val
		}
		if max < val {
			max = val
		}
	}

	t.Logf("Output: %0.3f\n", output)
	t.Logf("Min: %0.3f Max: %0.3f\n", min, max)
}

func BenchmarkDecode(b *testing.B) {
	d := NewDecoder(NewPacketConfig(72), 1)

	block := make([]byte, d.DecCfg.BlockSize2)

	b.SetBytes(int64(d.DecCfg.BlockSize))
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = d.Decode(block)
	}
}

func BenchmarkDecodeUint(b *testing.B) {
	d := NewDecoder(NewPacketConfig(72), 1)

	block := make([]byte, d.DecCfg.BlockSize2)

	b.SetBytes(int64(d.DecCfg.BlockSize))
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = d.DecodeUint(block)
	}
}

func BenchmarkTranspose(b *testing.B) {
	d := NewDecoder(NewPacketConfig(72), 1)

	b.SetBytes(int64(d.DecCfg.BlockSize))
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		d.Transpose(d.Quantized)
	}
}
