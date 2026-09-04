package domain

import "WorkBaby/internal/pkg"

// MediaKind 媒体模态。
type MediaKind string

const (
	MediaKindImage   MediaKind = "image"
	MediaKindVideo   MediaKind = "video"
	MediaKindAudio   MediaKind = "audio"
	MediaKindModel3D MediaKind = "model3d"
	MediaKindVFX     MediaKind = "vfx"
)

// MediaPresetDO 媒体预设（media_presets 表；参数模板）。
type MediaPresetDO struct {
	ID         string    `gorm:"primaryKey;size:64" json:"id"`
	Name       string    `gorm:"size:128;not null" json:"name"`
	Kind       MediaKind `gorm:"size:32;not null"  json:"kind"`
	Backend    string    `gorm:"size:32;not null"  json:"backend"`
	Model      string    `gorm:"size:128"          json:"model"`
	ParamsJSON string    `gorm:"type:text"         json:"params_json"`
	IsDefault  bool      `gorm:"default:false"     json:"is_default"`
	Enabled    bool      `gorm:"default:true"      json:"enabled"`
	CreatedAt  int64     `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt  int64     `gorm:"autoUpdateTime:milli" json:"updated_at"`
	DeletedAt  int64     `gorm:"default:0;index"   json:"deleted_at"` // 软删；unix milli
}

// TableName 固定表名。
func (MediaPresetDO) TableName() string { return "media_presets" }

// MediaPresetREQ 创建/更新请求。
type MediaPresetREQ struct {
	Name       string    `json:"name"`
	Kind       MediaKind `json:"kind"`
	Backend    string    `json:"backend"`
	Model      string    `json:"model"`
	ParamsJSON string    `json:"params_json"`
	IsDefault  bool      `json:"is_default"`
	Enabled    *bool     `json:"enabled"`
}

// MediaPresetRESP 出参（对齐前端 MediaPreset）。
type MediaPresetRESP struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Kind       MediaKind `json:"kind"`
	Backend    string    `json:"backend"`
	Model      string    `json:"model"`
	ParamsJSON string    `json:"params_json"`
	IsDefault  bool      `json:"is_default"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  int64     `json:"created_at"`
	UpdatedAt  int64     `json:"updated_at"`
}

// MediaArtifactDO 媒体产物（media_artifacts 表；文件落 {home}/media/{YYYY-MM}/{id}.{ext}）。
type MediaArtifactDO struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	Kind      MediaKind `gorm:"size:32;index"      json:"kind"`
	PresetID  string    `gorm:"size:64"            json:"preset_id"`
	Prompt    string    `gorm:"type:text"          json:"prompt"`
	FilePath  string    `gorm:"size:512"           json:"-"` // 磁盘绝对路径，不回传前端
	MimeType  string    `gorm:"size:64"            json:"mime_type"`
	Width     *int      `json:"width"`
	Height    *int      `json:"height"`
	FileSize  int64     `gorm:"default:0"          json:"file_size"`
	CreatedAt int64     `gorm:"autoCreateTime:milli" json:"created_at"`
}

// TableName 固定表名。
func (MediaArtifactDO) TableName() string { return "media_artifacts" }

// MediaGenerateREQ 生成请求（对齐前端 MediaGenerateReq）。
type MediaGenerateREQ struct {
	Kind     MediaKind `json:"kind"`
	Prompt   string    `json:"prompt"`
	PresetID string    `json:"preset_id"`
}

// MediaArtifactRESP 出参（对齐前端 MediaArtifact；url 经 AssetServer /files/media/{id} 暴露）。
type MediaArtifactRESP struct {
	ID          string    `json:"id"`
	Kind        MediaKind `json:"kind"`
	PresetID    string    `json:"preset_id"`
	Prompt      string    `json:"prompt"`
	PreviewURL  string    `json:"preview_url"`
	DownloadURL string    `json:"download_url"`
	MimeType    string    `json:"mime_type"`
	Width       *int      `json:"width"`
	Height      *int      `json:"height"`
	FileSize    int64     `json:"file_size"`
	CreatedAt   int64     `json:"created_at"`
}

// 错误变量；段位 9000-9006。
var (
	ErrMediaInvalidKind    = pkg.New(9000, "unsupported media kind", "")
	ErrMediaGenerate       = pkg.New(9001, "media generate failed", "")
	ErrMediaPresetNotFound = pkg.New(9004, "media preset not found", "")
	ErrMediaNotFound       = pkg.New(9004, "media artifact not found", "")
)
