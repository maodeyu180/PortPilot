package control

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

const passwordIterations = 600000

var errAlreadyInitialized = errors.New("管理密码已经设置，请直接登录")

type adminRecord struct {
	Version    int    `json:"version"`
	Pending    bool   `json:"pending,omitempty"`
	Salt       string `json:"salt,omitempty"`
	Hash       string `json:"hash,omitempty"`
	Iterations int    `json:"iterations,omitempty"`
}

type adminAuth struct {
	mu     sync.RWMutex
	path   string
	record adminRecord
	ready  chan struct{}
}

func validateAdminPassword(password string) error {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len(password) > 1024 || strings.TrimSpace(password) == "" {
		return fmt.Errorf("管理密码至少需要 12 个字符，且不能超过 1024 字节或全部为空格")
	}
	return nil
}

func randomHex(size int) string {
	buf := make([]byte, size)
	// crypto/rand.Read terminates the process if secure randomness is unavailable.
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func passwordRecord(password string) (adminRecord, error) {
	salt := randomHex(16)
	key, err := pbkdf2.Key(sha256.New, password, []byte(salt), passwordIterations, 32)
	if err != nil {
		return adminRecord{}, err
	}
	return adminRecord{Version: 1, Salt: salt, Hash: hex.EncodeToString(key), Iterations: passwordIterations}, nil
}

func matchesPassword(record adminRecord, password string) bool {
	if record.Hash == "" || len(password) > 1024 {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, password, []byte(record.Salt), record.Iterations, 32)
	expected, decodeErr := hex.DecodeString(record.Hash)
	return err == nil && decodeErr == nil && hmac.Equal(key, expected)
}

func validateAdminRecord(record adminRecord) error {
	if record.Version != 1 {
		return fmt.Errorf("不支持的管理认证数据版本")
	}
	if record.Pending {
		if record.Hash != "" || record.Salt != "" || record.Iterations != 0 {
			return fmt.Errorf("初始化认证数据无效")
		}
		return nil
	}
	salt, saltErr := hex.DecodeString(record.Salt)
	hash, hashErr := hex.DecodeString(record.Hash)
	if saltErr != nil || hashErr != nil || len(salt) != 16 || len(hash) != 32 || record.Iterations < passwordIterations || record.Iterations > 2000000 {
		return fmt.Errorf("管理密码数据无效")
	}
	return nil
}

func openAdmin(dir, password string) (*adminAuth, error) {
	if password != "" {
		if err := validateAdminPassword(password); err != nil {
			return nil, err
		}
	}
	auth := &adminAuth{path: filepath.Join(dir, "admin.json"), ready: make(chan struct{})}
	dirty := false
	info, err := os.Lstat(auth.path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > 4096 {
			return nil, fmt.Errorf("管理认证文件无效，原文件已保留")
		}
		data, readErr := os.ReadFile(auth.path)
		if readErr != nil {
			return nil, readErr
		}
		var saved struct {
			adminRecord
			LegacySetupToken string `json:"setupToken"`
		}
		if json.Unmarshal(data, &saved) != nil {
			return nil, fmt.Errorf("管理认证文件损坏，原文件已保留；请恢复备份")
		}
		auth.record = saved.adminRecord
		// Migrate pending installations created before direct password setup.
		if saved.LegacySetupToken != "" {
			key, decodeErr := hex.DecodeString(saved.LegacySetupToken)
			if decodeErr != nil || len(key) != 32 || auth.record.Pending || auth.record.Hash != "" || auth.record.Salt != "" || auth.record.Iterations != 0 {
				return nil, fmt.Errorf("管理认证文件损坏，原文件已保留；请恢复备份")
			}
			auth.record.Pending = true
			dirty = true
		}
		if validateAdminRecord(auth.record) != nil {
			return nil, fmt.Errorf("管理认证文件损坏，原文件已保留；请恢复备份")
		}
		if err := os.Chmod(auth.path, 0600); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	} else {
		auth.record = adminRecord{Version: 1, Pending: true}
		dirty = true
	}
	if password != "" && !matchesPassword(auth.record, password) {
		auth.record, err = passwordRecord(password)
		if err != nil {
			return nil, err
		}
		dirty = true
	}
	if dirty {
		if err := auth.save(auth.record); err != nil {
			return nil, err
		}
	}
	if auth.record.Hash != "" {
		close(auth.ready)
	}
	return auth, nil
}

func (a *adminAuth) save(record adminRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(a.path, data)
}

func (a *adminAuth) setupRequired() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.record.Pending
}

func (a *adminAuth) verify(password string) bool {
	a.mu.RLock()
	record := a.record
	a.mu.RUnlock()
	return matchesPassword(record, password)
}

func (a *adminAuth) initialize(password string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.record.Hash != "" {
		return errAlreadyInitialized
	}
	if err := validateAdminPassword(password); err != nil {
		return err
	}
	record, err := passwordRecord(password)
	if err != nil {
		return err
	}
	if err := a.save(record); err != nil {
		return err
	}
	a.record = record
	close(a.ready)
	return nil
}
