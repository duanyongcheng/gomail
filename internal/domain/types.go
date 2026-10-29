package domain

import "context"

type Capabilities struct {
	Read        bool `json:"read"`
	ModifyFlags bool `json:"modify_flags"`
	Send        bool `json:"send"`
	HTML        bool `json:"html"`
}

type Account struct {
	ID                  string       `json:"id"`
	Email               string       `json:"email"`
	DisplayName         string       `json:"display_name"`
	Color               *string      `json:"color"`
	Provider            string       `json:"provider"`
	AccountLabel        *string      `json:"account_label"`
	ProviderDisplayName *string      `json:"provider_display_name"`
	OAuthSubject        *string      `json:"-"`
	CreatedAt           int64        `json:"created_at"`
	UpdatedAt           int64        `json:"updated_at"`
	SortOrder           int          `json:"sort_order"`
	Capabilities        Capabilities `json:"capabilities"`
}

type Folder struct {
	ID         string  `json:"id"`
	AccountID  string  `json:"account_id"`
	RemoteID   string  `json:"remote_id"`
	Name       string  `json:"name"`
	FolderType string  `json:"folder_type"`
	Role       *string `json:"role"`
	ParentID   *string `json:"parent_id"`
	Color      *string `json:"color"`
	IsSystem   bool    `json:"is_system"`
	SortOrder  int     `json:"sort_order"`
}

type Address struct {
	Name    *string `json:"name"`
	Address string  `json:"address"`
}

type Message struct {
	Capabilities     Capabilities `json:"capabilities"`
	ID               string       `json:"id"`
	AccountID        string       `json:"account_id"`
	RemoteID         string       `json:"remote_id"`
	MessageIDHeader  *string      `json:"message_id_header"`
	InReplyTo        *string      `json:"in_reply_to"`
	ReferencesHeader *string      `json:"references_header"`
	ThreadID         *string      `json:"thread_id"`
	Subject          string       `json:"subject"`
	Snippet          string       `json:"snippet"`
	FromAddress      string       `json:"from_address"`
	FromName         string       `json:"from_name"`
	To               []Address    `json:"to_list"`
	CC               []Address    `json:"cc_list"`
	BCC              []Address    `json:"bcc_list"`
	BodyText         string       `json:"body_text"`
	BodyHTML         string       `json:"body_html_raw"`
	HasAttachments   bool         `json:"has_attachments"`
	IsRead           bool         `json:"is_read"`
	IsStarred        bool         `json:"is_starred"`
	IsDraft          bool         `json:"is_draft"`
	Date             int64        `json:"date"`
	RemoteVersion    *string      `json:"remote_version"`
	IsDeleted        bool         `json:"is_deleted"`
	DeletedAt        *int64       `json:"deleted_at"`
	CreatedAt        int64        `json:"created_at"`
	UpdatedAt        int64        `json:"updated_at"`
}

type ThreadSummary struct {
	ThreadID       string   `json:"thread_id"`
	AccountID      string   `json:"account_id"`
	Subject        string   `json:"subject"`
	Snippet        string   `json:"snippet"`
	LastDate       int64    `json:"last_date"`
	MessageCount   int      `json:"message_count"`
	UnreadCount    int      `json:"unread_count"`
	IsStarred      bool     `json:"is_starred"`
	Participants   []string `json:"participants"`
	HasAttachments bool     `json:"has_attachments"`
}

type Attachment struct {
	ID        string  `json:"id"`
	MessageID string  `json:"message_id"`
	Filename  string  `json:"filename"`
	MIMEType  string  `json:"mime_type"`
	Size      int64   `json:"size"`
	LocalPath *string `json:"local_path"`
	ContentID *string `json:"content_id"`
	Inline    bool    `json:"is_inline"`
	Data      []byte  `json:"-"`
}

type SearchHit struct {
	MessageID   string  `json:"message_id"`
	Score       float64 `json:"score"`
	Snippet     string  `json:"snippet"`
	Subject     string  `json:"subject"`
	FromAddress string  `json:"from_address"`
	Date        int64   `json:"date"`
}

type DownloadFailure struct {
	AccountID        string `json:"account_id"`
	RemoteID         string `json:"remote_id"`
	AccountEmail     string `json:"account_email"`
	Subject          string `json:"subject"`
	Kind             string `json:"kind"`
	Attempts         int    `json:"attempts"`
	NextRetryAt      int64  `json:"next_retry_at"`
	UpdatedAt        int64  `json:"updated_at"`
	NotifyOnRecovery bool   `json:"-"`
}

type DownloadFailuresPage struct {
	Items []DownloadFailure `json:"items"`
	Total int               `json:"total"`
}

type SyncProgress struct {
	AccountID string `json:"account_id"`
	Status    string `json:"status"`
	Phase     string `json:"phase"`
	Count     int    `json:"count"`
	Message   string `json:"message"`
}

type NewMail struct {
	AccountID  string   `json:"account_id"`
	MessageID  string   `json:"message_id"`
	FolderIDs  []string `json:"folder_ids"`
	ThreadID   *string  `json:"thread_id"`
	Subject    string   `json:"subject"`
	From       string   `json:"from"`
	ReceivedAt int64    `json:"received_at"`
}

type SyncResult struct {
	DeferredCount int       `json:"deferred_count"`
	NewMessages   []NewMail `json:"-"`
	Count         int       `json:"count"`
	HistoryID     string    `json:"history_id"`
	Initial       bool      `json:"initial"`
	Limited       bool      `json:"limited"`
}

type Transport interface {
	Capabilities() Capabilities
	Sync(context.Context) (SyncResult, error)
	Flags(context.Context, string, *bool, *bool) error
}

type ProgressTransport interface {
	SyncWithProgress(context.Context, func(SyncProgress)) (SyncResult, error)
}

type FolderProvider interface {
	Folders(context.Context) ([]Folder, error)
}
type LabelProvider interface {
	CreateLabel(context.Context, string) (Folder, error)
}
type DraftProvider interface {
	SaveDraft(context.Context, Message) (string, error)
}
