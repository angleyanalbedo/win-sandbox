package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SandboxRecord 沙箱持久化记录
type SandboxRecord struct {
	ID          string    `json:"id"`
	ContainerID string    `json:"container_id"` // HCS 容器名
	ImageRef    string    `json:"image_ref"`
	Status      string    `json:"status"` // created/running/stopped
	CreatedAt   time.Time `json:"created_at"`
	ScratchID   string    `json:"scratch_id"`
	ScratchPath string    `json:"scratch_path"`
	LayerPath   string    `json:"layer_path"` // 基础层路径
	MemoryMB    int       `json:"memory_mb"`
	CPUs        int       `json:"cpus"`
	Network     string    `json:"network"`
}

// Store 沙箱状态存储接口
type Store interface {
	Save(record *SandboxRecord) error
	Load(id string) (*SandboxRecord, error)
	Delete(id string) error
	List() ([]*SandboxRecord, error)
	UpdateStatus(id string, status string) error
}

// FileStore 基于文件的状态存储
type FileStore struct {
	dir string
}

// NewFileStore 创建文件状态存储
// 默认路径: %USERPROFILE%\.win-sandbox\sandboxes\
func NewFileStore(dir string) (*FileStore, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("获取用户目录失败: %w", err)
		}
		dir = filepath.Join(home, ".win-sandbox", "sandboxes")
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建状态目录失败: %w", err)
	}

	return &FileStore{dir: dir}, nil
}

// Save 保存沙箱记录
func (s *FileStore) Save(record *SandboxRecord) error {
	path := s.filePath(record.ID)
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化记录失败: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入状态文件失败: %w", err)
	}

	return nil
}

// Load 加载沙箱记录
func (s *FileStore) Load(id string) (*SandboxRecord, error) {
	path := s.filePath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("沙箱 %s 不存在", id)
		}
		return nil, fmt.Errorf("读取状态文件失败: %w", err)
	}

	var record SandboxRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("解析状态文件失败: %w", err)
	}

	return &record, nil
}

// Delete 删除沙箱记录
func (s *FileStore) Delete(id string) error {
	path := s.filePath(id)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil // 已经不存在，视为成功
		}
		return fmt.Errorf("删除状态文件失败: %w", err)
	}
	return nil
}

// List 列出所有沙箱记录
func (s *FileStore) List() ([]*SandboxRecord, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("读取状态目录失败: %w", err)
	}

	var records []*SandboxRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		id := entry.Name()[:len(entry.Name())-5] // 去掉 .json
		record, err := s.Load(id)
		if err != nil {
			// 跳过损坏的记录
			continue
		}
		records = append(records, record)
	}

	return records, nil
}

// UpdateStatus 更新沙箱状态
func (s *FileStore) UpdateStatus(id string, status string) error {
	record, err := s.Load(id)
	if err != nil {
		return err
	}
	record.Status = status
	return s.Save(record)
}

func (s *FileStore) filePath(id string) string {
	return filepath.Join(s.dir, id+".json")
}
