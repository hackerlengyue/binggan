package engine

import (
	"encoding/binary"
	"errors"
	"fmt"
	mp4 "github.com/abema/go-mp4"
	"io"
	"sort"
)

type atom struct {
	kind                string
	start, end, payload int64
}
type Sample struct {
	Offset int64
	Size   int
	Video  bool
}

func atoms(r io.ReaderAt, start, end int64) ([]atom, error) {
	out := make([]atom, 0)
	section := io.NewSectionReader(r, start, end-start)
	for p := start; p < end; {
		info, err := mp4.ReadBoxInfo(section)
		if err != nil {
			return nil, err
		}
		if info.Size < info.HeaderSize || info.Size > uint64(end-p) {
			return nil, errors.New("MP4 盒子大小越界")
		}
		out = append(out, atom{info.Type.String(), p, p + int64(info.Size), p + int64(info.HeaderSize)})
		p += int64(info.Size)
		if _, err = section.Seek(p-start, io.SeekStart); err != nil {
			return nil, err
		}
		if len(out) > 100000 {
			return nil, errors.New("MP4 盒子数量过多")
		}
	}
	return out, nil
}
func child(r io.ReaderAt, parent atom, name string) (atom, error) {
	list, err := atoms(r, parent.payload, parent.end)
	if err != nil {
		return atom{}, err
	}
	for _, b := range list {
		if b.kind == name {
			return b, nil
		}
	}
	return atom{}, fmt.Errorf("MP4 缺少 %s", name)
}
func payload(r io.ReaderAt, a atom, max int64) ([]byte, error) {
	n := a.end - a.payload
	if n > max || n < 0 {
		return nil, errors.New("MP4 元数据超过限制")
	}
	b := make([]byte, n)
	_, err := r.ReadAt(b, a.payload)
	return b, err
}
func Samples(r io.ReaderAt, size int64) ([]Sample, error) {
	top, err := atoms(r, 0, size)
	if err != nil {
		return nil, err
	}
	var moov atom
	var mdats []atom
	ftyp := false
	for _, a := range top {
		switch a.kind {
		case "ftyp":
			ftyp = true
		case "moov":
			moov = a
		case "mdat":
			mdats = append(mdats, a)
		case "moof":
			return nil, errors.New("暂不支持 fragmented MP4")
		}
	}
	if !ftyp || moov.end == 0 || len(mdats) == 0 {
		return nil, errors.New("缺少 ftyp、moov 或 mdat")
	}
	tracks, err := atoms(r, moov.payload, moov.end)
	if err != nil {
		return nil, err
	}
	out := make([]Sample, 0)
	for _, track := range tracks {
		if track.kind != "trak" {
			continue
		}
		mdia, e := child(r, track, "mdia")
		if e != nil {
			return nil, e
		}
		hdlr, e := child(r, mdia, "hdlr")
		if e != nil {
			return nil, e
		}
		h, e := payload(r, hdlr, 1<<20)
		if e != nil || len(h) < 12 {
			return nil, errors.New("MP4 hdlr 无效")
		}
		video := string(h[8:12]) == "vide"
		minf, e := child(r, mdia, "minf")
		if e != nil {
			return nil, e
		}
		stbl, e := child(r, minf, "stbl")
		if e != nil {
			return nil, e
		}
		sz, e := child(r, stbl, "stsz")
		if e != nil {
			return nil, e
		}
		b, e := payload(r, sz, 128<<20)
		if e != nil || len(b) < 12 {
			return nil, errors.New("MP4 stsz 无效")
		}
		fixed := binary.BigEndian.Uint32(b[4:8])
		count := int(binary.BigEndian.Uint32(b[8:12]))
		if count > 5000000 || len(out)+count > 5000000 || fixed == 0 && count > (len(b)-12)/4 {
			return nil, errors.New("MP4 sample 数量无效或超过限制")
		}
		if count == 0 {
			continue
		}
		sizes := make([]uint32, count)
		for i := range sizes {
			sizes[i] = fixed
			if fixed == 0 {
				sizes[i] = binary.BigEndian.Uint32(b[12+i*4:])
			}
		}
		co, e := child(r, stbl, "co64")
		step := 8
		if e != nil {
			co, e = child(r, stbl, "stco")
			step = 4
		}
		if e != nil {
			return nil, e
		}
		b, e = payload(r, co, 128<<20)
		if e != nil || len(b) < 8 {
			return nil, errors.New("MP4 chunk 表无效")
		}
		n := int(binary.BigEndian.Uint32(b[4:8]))
		if n > count || n > (len(b)-8)/step {
			return nil, errors.New("MP4 chunk 表越界")
		}
		offsets := make([]uint64, n)
		for i := range offsets {
			if step == 8 {
				offsets[i] = binary.BigEndian.Uint64(b[8+i*8:])
			} else {
				offsets[i] = uint64(binary.BigEndian.Uint32(b[8+i*4:]))
			}
		}
		sc, e := child(r, stbl, "stsc")
		if e != nil {
			return nil, e
		}
		b, e = payload(r, sc, 64<<20)
		if e != nil || len(b) < 8 {
			return nil, errors.New("MP4 stsc 无效")
		}
		entries := int(binary.BigEndian.Uint32(b[4:8]))
		if entries == 0 || entries > (len(b)-8)/12 {
			return nil, errors.New("MP4 stsc 数量无效")
		}
		sampleIndex := 0
		prev := 0
		for i := 0; i < entries; i++ {
			first := int(binary.BigEndian.Uint32(b[8+i*12:]))
			per := int(binary.BigEndian.Uint32(b[12+i*12:]))
			end := len(offsets) + 1
			if i+1 < entries {
				end = int(binary.BigEndian.Uint32(b[8+(i+1)*12:]))
			}
			if first <= prev || i == 0 && first != 1 || end <= first || end > len(offsets)+1 || per <= 0 {
				return nil, errors.New("MP4 sample/chunk 映射无效")
			}
			prev = first
			for chunk := first; chunk < end; chunk++ {
				off := offsets[chunk-1]
				for j := 0; j < per; j++ {
					if sampleIndex >= len(sizes) {
						return nil, errors.New("MP4 sample 数量不匹配")
					}
					sz := int(sizes[sampleIndex])
					sampleIndex++
					if sz < 1 || sz > 64<<20 || off > uint64(size) || uint64(sz) > uint64(size)-off {
						return nil, errors.New("MP4 sample 越界或过大")
					}
					valid := false
					for _, m := range mdats {
						if off >= uint64(m.payload) && off+uint64(sz) <= uint64(m.end) {
							valid = true
							break
						}
					}
					if !valid {
						return nil, errors.New("MP4 sample 不在媒体数据区")
					}
					out = append(out, Sample{int64(off), sz, video})
					off += uint64(sz)
				}
			}
		}
		if sampleIndex != len(sizes) {
			return nil, errors.New("MP4 sample 表不完整")
		}
	}
	if len(out) == 0 {
		return nil, errors.New("未找到可解密的 sample")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Offset < out[j].Offset })
	for i := 1; i < len(out); i++ {
		if out[i].Offset < out[i-1].Offset+int64(out[i-1].Size) {
			return nil, errors.New("MP4 sample 重叠")
		}
	}
	return out, nil
}
func ValidateNAL(r io.ReaderAt, samples []Sample) (float64, error) {
	checked, good := 0, 0
	for _, s := range samples {
		if !s.Video {
			continue
		}
		b := make([]byte, s.Size)
		if _, err := r.ReadAt(b, s.Offset); err != nil {
			return 0, err
		}
		valid := true
		p := 0
		for p < len(b) {
			if len(b)-p < 4 {
				valid = false
				break
			}
			n := int(binary.BigEndian.Uint32(b[p:]))
			p += 4
			if n == 0 || n > len(b)-p {
				valid = false
				break
			}
			t := b[p] & 31
			if t != 1 && t != 5 && (t < 6 || t > 12) {
				valid = false
				break
			}
			p += n
		}
		checked++
		if valid {
			good++
		}
		if checked >= 3000 {
			break
		}
	}
	if checked == 0 {
		return 0, errors.New("没有可验证的 H.264 视频帧")
	}
	return float64(good) / float64(checked), nil
}
