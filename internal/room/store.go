package room

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// 端口分配区间。四个区间互不重叠，
// 因此同一台机器上开多个房间时不会互相抢占。
const (
	serverPortBase      = 11000 // 游戏端口
	masterPortBase      = 21000 // 分片协调端口（cluster.ini [SHARD] master_port）
	steamMasterPortBase = 31000 // Steam 主服务器端口
	steamAuthPortBase   = 41000 // Steam 认证端口
	roomsFileVersion    = 1
)

// DefaultStorePath 返回房间定义文件的默认位置：<rootDir>/rooms.json。
// 放在存档根目录下，便于连同存档一起搬迁。
func DefaultStorePath(rootDir string) string {
	return filepath.Join(rootDir, "rooms.json")
}

// fileFormat 是 rooms.json 的落盘结构。
// 用带版本号的对象而不是裸数组，方便以后加字段时做迁移。
type fileFormat struct {
	Version int     `json:"version"`
	Rooms   []*Room `json:"rooms"`
}

// Store 负责房间定义的持久化。
// 这里需要一个 JSON 文件：
// 自用场景房间数量是个位数，也没有并发写入，引入数据库得不偿失。
type Store struct {
	path string

	mu    sync.RWMutex
	rooms []*Room
}

// NewStore 打开（或创建）房间定义文件。
// 文件不存在时返回空集合，不视为错误。
func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Path 返回定义文件的绝对路径，供接口展示。
func (s *Store) Path() string { return s.path }

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取房间定义失败: %w", err)
	}
	// 兼容带 BOM 的文件（用户可能手工编辑过）
	text := strings.TrimPrefix(string(data), "\xEF\xBB\xBF")
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var f fileFormat
	if err := json.Unmarshal([]byte(text), &f); err != nil {
		return fmt.Errorf("解析房间定义失败: %w", err)
	}
	for _, r := range f.Rooms {
		if r == nil {
			continue
		}
		r.Normalize()
		s.rooms = append(s.rooms, r)
	}
	return nil
}

// save 原子写入房间定义。调用方必须已持有写锁。
func (s *Store) save() error {
	sort.Slice(s.rooms, func(i, j int) bool { return s.rooms[i].ID < s.rooms[j].ID })
	data, err := json.MarshalIndent(fileFormat{Version: roomsFileVersion, Rooms: s.rooms}, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化房间定义失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return fmt.Errorf("创建房间定义目录失败: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("写入房间定义失败: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换房间定义失败: %w", err)
	}
	return nil
}

// List 返回全部房间的副本，按 ID 升序。
func (s *Store) List() []*Room {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		out = append(out, clone(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get 按 ID 取房间。
func (s *Store) Get(id int) (*Room, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.rooms {
		if r.ID == id {
			return clone(r), true
		}
	}
	return nil, false
}

// Create 新建房间：分配 ID 与端口、校验、落盘。
func (s *Store) Create(r *Room) (*Room, error) {
	if r == nil {
		return nil, fmt.Errorf("房间不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	r.ID = s.nextID()
	r.Normalize()
	// 端口必须在 Normalize 之后分配，否则会被 Normalize 的兜底值覆盖
	s.allocPorts(r)

	if err := r.Validate(); err != nil {
		return nil, err
	}
	if dup := r.selfConflict(); len(dup) > 0 {
		return nil, fmt.Errorf("端口配置重复: %v", dup)
	}
	if c := r.ConflictPorts(s.rooms, 0); len(c) > 0 {
		return nil, fmt.Errorf("端口已被其他房间占用: %v", c)
	}

	s.rooms = append(s.rooms, r)
	if err := s.save(); err != nil {
		s.rooms = s.rooms[:len(s.rooms)-1]
		return nil, err
	}
	return clone(r), nil
}

// Update 保存房间的修改。ID 不存在时返回错误。
func (s *Store) Update(r *Room) (*Room, error) {
	if r == nil {
		return nil, fmt.Errorf("房间不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, e := range s.rooms {
		if e.ID == r.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("房间不存在: %d", r.ID)
	}

	// 补齐端口：编辑时前端可能没回传未启用分片的端口
	old := s.rooms[idx]
	fillMissingShardPorts(r, old)

	r.Normalize()
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if dup := r.selfConflict(); len(dup) > 0 {
		return nil, fmt.Errorf("端口配置重复: %v", dup)
	}
	if c := r.ConflictPorts(s.rooms, r.ID); len(c) > 0 {
		return nil, fmt.Errorf("端口已被其他房间占用: %v", c)
	}

	s.rooms[idx] = r
	if err := s.save(); err != nil {
		s.rooms[idx] = old
		return nil, err
	}
	return clone(r), nil
}

// Delete 删除房间定义。
// 只删除定义，不动磁盘上的存档目录——存档是玩家心血，
// 误删无法挽回，需要清理时由用户手工处理。
func (s *Store) Delete(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, r := range s.rooms {
		if r.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("房间不存在: %d", id)
	}
	s.rooms = append(s.rooms[:idx], s.rooms[idx+1:]...)
	return s.save()
}

// nextID 返回未被占用的最小正 ID。调用方必须已持有写锁。
// 复用空洞而不是简单地 max+1：删掉房间后重建能拿到较小 ID，
// 目录名也保持紧凑。
func (s *Store) nextID() int {
	used := make(map[int]bool, len(s.rooms))
	for _, r := range s.rooms {
		used[r.ID] = true
	}
	for id := 1; ; id++ {
		if !used[id] {
			return id
		}
	}
}

// allocPorts 为新房间在四个区间内各分配一组全局唯一的端口。
// 无论洞穴是否启用都分配，这样以后启用洞穴时端口依然稳定。
func (s *Store) allocPorts(r *Room) {
	used := s.usedPorts(0)
	next := func(base int) int {
		p := base
		for used[p] {
			p++
		}
		used[p] = true
		return p
	}

	r.MasterPort = next(masterPortBase)

	shards := defaultShards()
	for i := range shards {
		shards[i].ServerPort = next(serverPortBase)
		shards[i].MasterServerPort = next(steamMasterPortBase)
		shards[i].AuthenticationPort = next(steamAuthPortBase)
	}
	r.Shards = shards
}

// usedPorts 汇总所有房间占用的端口。excludeID 用于编辑时跳过自身。
func (s *Store) usedPorts(excludeID int) map[int]bool {
	used := make(map[int]bool)
	for _, r := range s.rooms {
		if r.ID == excludeID {
			continue
		}
		for _, sh := range r.Shards {
			used[sh.ServerPort] = true
			used[sh.MasterServerPort] = true
			used[sh.AuthenticationPort] = true
		}
		used[r.MasterPort] = true
	}
	return used
}

// fillMissingShardPorts 用旧值补齐前端未回传的端口。
// 分片协调端口同样要补，否则 Normalize 会把它兜底成 21000，
// 在多房间场景下可能与别的房间撞号。
func fillMissingShardPorts(newRoom, old *Room) {
	if newRoom.MasterPort == 0 {
		newRoom.MasterPort = old.MasterPort
	}
	if len(newRoom.Shards) == 0 {
		newRoom.Shards = old.Shards
		return
	}
	for i := range newRoom.Shards {
		for _, o := range old.Shards {
			if o.Name != newRoom.Shards[i].Name {
				continue
			}
			if newRoom.Shards[i].ServerPort == 0 {
				newRoom.Shards[i].ServerPort = o.ServerPort
			}
			if newRoom.Shards[i].MasterServerPort == 0 {
				newRoom.Shards[i].MasterServerPort = o.MasterServerPort
			}
			if newRoom.Shards[i].AuthenticationPort == 0 {
				newRoom.Shards[i].AuthenticationPort = o.AuthenticationPort
			}
		}
	}
}

// clone 深拷贝一个房间，避免调用方改到 Store 内部状态。
func clone(r *Room) *Room {
	if r == nil {
		return nil
	}
	c := *r
	c.Shards = append([]Shard(nil), r.Shards...)
	return &c
}
