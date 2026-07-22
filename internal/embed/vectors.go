package embed

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"openknowledge/internal/entry"
)

type EntryVector struct {
	ModTime int64     `json:"mod_time"`
	Vector  []float32 `json:"vector"`
}

type VectorSet struct {
	Vectors map[string]*EntryVector `json:"vectors"`
}

func LoadVectors(path string) (*VectorSet, error) {
	vs := &VectorSet{Vectors: map[string]*EntryVector{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return vs, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, vs); err != nil {
		return nil, err
	}
	return vs, nil
}

func (vs *VectorSet) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(vs)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Update 按文件 mtime 增量更新条目向量，并清理已删除条目的向量。
func (vs *VectorSet) Update(ctx context.Context, c Client, entries []*entry.Entry) error {
	alive := map[string]bool{}
	for _, e := range entries {
		name := e.FileName()
		alive[name] = true
		fi, err := os.Stat(e.Path)
		if err != nil {
			return err
		}
		mtime := fi.ModTime().Unix()
		if v, ok := vs.Vectors[name]; ok && v.ModTime == mtime {
			continue
		}
		vec, err := c.Embed(ctx, e.EmbedText())
		if err != nil {
			return err
		}
		vs.Vectors[name] = &EntryVector{ModTime: mtime, Vector: vec}
	}
	for name := range vs.Vectors {
		if !alive[name] {
			delete(vs.Vectors, name)
		}
	}
	return nil
}
