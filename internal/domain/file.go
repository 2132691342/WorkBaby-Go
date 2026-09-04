package domain

import "WorkBaby/internal/pkg"

// FileType 文件分类。
type FileType string

const (
	FileTypeDocument FileType = "document"
	FileTypeImage    FileType = "image"
	FileTypeAudio    FileType = "audio"
	FileTypeVideo    FileType = "video"
	FileTypeArchive  FileType = "archive"
	FileTypeData     FileType = "data"
	FileTypeCode     FileType = "code"
	FileTypeOther    FileType = "other"
)

// FileStatus 文件状态（MVP 固定 uploaded）。
type FileStatus string

const FileStatusUploaded FileStatus = "uploaded"

// DetectFileType 按 MIME + 扩展名推断文件分类（MIME 优先，其次扩展名）。
func DetectFileType(name, mime string) FileType {
	if mime != "" {
		switch {
		case hasPrefix(mime, "image/"):
			return FileTypeImage
		case hasPrefix(mime, "audio/"):
			return FileTypeAudio
		case hasPrefix(mime, "video/"):
			return FileTypeVideo
		}
	}
	ext := extOf(name)
	switch ext {
	case "pdf", "doc", "docx", "ppt", "pptx", "xls", "xlsx", "txt", "md", "rtf":
		return FileTypeDocument
	case "zip", "tar", "gz", "7z", "rar":
		return FileTypeArchive
	case "json", "csv", "xml", "yaml", "yml", "toml":
		return FileTypeData
	case "java", "py", "go", "ts", "js", "tsx", "jsx", "c", "cpp", "h", "rs", "sh":
		return FileTypeCode
	default:
		return FileTypeOther
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func extOf(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return lower(name[i+1:])
		}
	}
	return ""
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// FileDO 文件（files 表；内容存 {home}/files/{id}，DB 只存元数据）。
type FileDO struct {
	ID           string     `gorm:"primaryKey;size:64"  json:"id"`
	Name         string     `gorm:"size:512;not null"   json:"name"`
	OriginalName string     `gorm:"size:512"            json:"original_name"`
	FileType     FileType   `gorm:"size:16;index"       json:"file_type"`
	MimeType     string     `gorm:"size:128"            json:"mime_type"`
	Size         int64      `gorm:"default:0"           json:"size"`
	Status       FileStatus `gorm:"size:16"          json:"status"`
	UserID       string     `gorm:"size:64;index"       json:"-"`
	SessionID    string     `gorm:"size:64;index"       json:"session_id,omitempty"`
	FolderID     string     `gorm:"size:64;index"       json:"folder_id,omitempty"`
	StoragePath  string     `gorm:"size:1024"           json:"-"` // 磁盘绝对路径，不回传
	CreatedAt    int64      `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt    int64      `gorm:"autoUpdateTime:milli" json:"updated_at"`
}

// TableName 固定表名。
func (FileDO) TableName() string { return "files" }

// FileRESP 出参（对齐前端 FileInfo）。
type FileRESP struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	OriginalName string     `json:"original_name"`
	FileType     FileType   `json:"file_type"`
	MimeType     string     `json:"mime_type"`
	Size         int64      `json:"size"`
	Status       FileStatus `json:"status"`
	// 不加 omitempty：Wails 会按此生成 `sessionId?: string`，
	// 与「空串即无归属」的实际契约不符，也让前端多一层 undefined 判定
	SessionID string `json:"session_id"`
	FolderID  string `json:"folder_id"`
	CreatedAt int64  `json:"created_at"`
}

// 错误变量；段位 1200-1299。
var (
	ErrFileNotFound   = pkg.New(1201, "file not found", "")
	ErrFileUploadFail = pkg.New(1202, "file upload failed", "")
	ErrFileTypeForbid = pkg.New(1203, "file type not allowed", "")
)

// WorkspaceFileItem 工作区文件条目。
type WorkspaceFileItem struct {
	Path       string `json:"path"` // 相对工作区根
	Name       string `json:"name"`
	Ext        string `json:"ext"`
	Kind       string `json:"kind"` // html / image / pdf / text / video / file
	Size       int64  `json:"size"`
	URL        string `json:"url"`
	ModifiedAt int64  `json:"modified_at"`
}
