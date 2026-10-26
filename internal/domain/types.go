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

type SyncResult struct {
	Count     int    `json:"count"`
	HistoryID string `json:"history_id"`
	Initial   bool   `json:"initial"`
	Limited   bool   `json:"limited"`
}

type Transport interface {
	Capabilities() Capabilities
	Sync(context.Context) (SyncResult, error)
	Flags(context.Context, string, *bool, *bool) error
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
