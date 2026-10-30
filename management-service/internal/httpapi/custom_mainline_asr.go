package httpapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"livecompanion/management/internal/speechasr"
	assetstorage "livecompanion/management/internal/storage"
	"os"
	"strings"
	"time"
)

const customASRChunkBytes int64 = 6 * 60 * 60 * 48000

type customASRPrefixKey struct{}

// Normalization produces PCM16/24k/mono. Parse chunks rather than assuming a
// 44-byte header: FFmpeg also writes LIST/JUNK chunks.
func customPCMData(file *os.File) (offset, size int64, err error) {
	info, statErr := file.Stat()
	if statErr != nil {
		return 0, 0, statErr
	}
	if info.Size() > (1<<32)-1 {
		return 0, 0, errors.New("录音转换后超过WAV音轨4GB上限，请缩短录音后分批处理")
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return
	}
	var header [12]byte
	if _, err = io.ReadFull(file, header[:]); err != nil {
		return
	}
	if string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return 0, 0, errors.New("规范化音频不是有效WAV")
	}
	formatOK := false
	for count := 0; count < 1000; count++ {
		var chunk [8]byte
		if _, err = io.ReadFull(file, chunk[:]); err != nil {
			return
		}
		n := int64(binary.LittleEndian.Uint32(chunk[4:]))
		position, e := file.Seek(0, io.SeekCurrent)
		if e != nil {
			return 0, 0, e
		}
		if string(chunk[:4]) == "fmt " {
			var format [16]byte
			if n < 16 {
				return 0, 0, errors.New("WAV格式块无效")
			}
			if _, err = io.ReadFull(file, format[:]); err != nil {
				return
			}
			formatOK = binary.LittleEndian.Uint16(format[:2]) == 1 && binary.LittleEndian.Uint16(format[2:4]) == 1 && binary.LittleEndian.Uint32(format[4:8]) == 24000 && binary.LittleEndian.Uint16(format[14:]) == 16
		}
		if string(chunk[:4]) == "data" {
			info, e := file.Stat()
			if e != nil {
				return 0, 0, e
			}
			if !formatOK || n <= 0 || n%2 != 0 || position+n > info.Size() {
				return 0, 0, errors.New("WAV音频数据不完整或格式不匹配")
			}
			return position, n, nil
		}
		if _, err = file.Seek(position+n+n%2, io.SeekStart); err != nil {
			return
		}
	}
	return 0, 0, errors.New("WAV音频块过多")
}

func customPCMHeader(size int64) []byte {
	header := make([]byte, 44)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(size+36))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], 24000)
	binary.LittleEndian.PutUint32(header[28:32], 48000)
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(size))
	return header
}

// A combined asset can exceed the ASR provider's per-request size/duration.
// Split only recognition input; retain one original playback asset and offset
// sentence timestamps by exact sample counts. No whole-file allocation.
func transcribeCustomPCM(ctx context.Context, store assetstorage.Store, path, key, partPrefix string, chunkBytes int64, transcribe func(context.Context, string) (speechasr.Result, error)) (speechasr.Result, int64, error) {
	if chunkBytes <= 0 || chunkBytes%2 != 0 {
		return speechasr.Result{}, 0, errors.New("无效的识别分片大小")
	}
	file, err := os.Open(path)
	if err != nil {
		return speechasr.Result{}, 0, err
	}
	defer file.Close()
	offset, size, err := customPCMData(file)
	if err != nil {
		return speechasr.Result{}, 0, err
	}
	duration := size * 1000 / 48000
	if size <= chunkBytes {
		url, err := store.SignedURL(ctx, key, 6*time.Hour)
		if err != nil {
			return speechasr.Result{}, duration, err
		}
		result, err := transcribe(ctx, url)
		return result, duration, err
	}
	private, ok := store.(interface {
		PutPrivate(context.Context, string, io.Reader, string) error
	})
	if !ok {
		return speechasr.Result{}, duration, errors.New("ASR分片需要私有OSS存储")
	}
	combined := speechasr.Result{}
	for start := int64(0); start < size; start += chunkBytes {
		if err = ctx.Err(); err != nil {
			return speechasr.Result{}, duration, err
		}
		length := min(chunkBytes, size-start)
		if partPrefix == "" {
			return speechasr.Result{}, duration, errors.New("大文件识别缺少临时存储范围")
		}
		partKey := fmt.Sprintf("%sasr-%06d.wav", partPrefix, start/chunkBytes)
		result, partErr := func() (speechasr.Result, error) {
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				_ = store.Delete(cleanup, partKey)
			}()
			reader := io.MultiReader(bytes.NewReader(customPCMHeader(length)), io.NewSectionReader(file, offset+start, length))
			if err := private.PutPrivate(ctx, partKey, reader, "audio/wav"); err != nil {
				return speechasr.Result{}, err
			}
			url, err := store.SignedURL(ctx, partKey, 6*time.Hour)
			if err != nil {
				return speechasr.Result{}, err
			}
			return transcribe(ctx, url)
		}()
		if partErr != nil {
			return speechasr.Result{}, duration, partErr
		}
		shift := start * 1000 / 48000
		for _, sentence := range result.Sentences {
			sentence.BeginTime += shift
			sentence.EndTime += shift
			combined.Sentences = append(combined.Sentences, sentence)
		}
		combined.Text += strings.TrimSpace(result.Text) + "\n"
	}
	combined.Text = strings.TrimSpace(combined.Text)
	return combined, duration, nil
}
