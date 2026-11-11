// Package gdrive implements Google Drive storage provider operations.
package gdrive

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jooservices/xflickr/internal/sync/app"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
)

const folderMimeType = "application/vnd.google-apps.folder"

var errContextRequired = errors.New("gdrive: context is required")

// FolderClient finds or creates albums in the root of My Drive.
type FolderClient struct {
	service *drive.Service
}

// NewFolderClient creates a folder client with an authenticated Drive service.
func NewFolderClient(service *drive.Service) (*FolderClient, error) {
	if service == nil {
		return nil, errors.New("gdrive: Drive service is required")
	}
	return &FolderClient{service: service}, nil
}

// EnsureAlbum returns the ID of a root folder with name, creating it if absent.
func (c *FolderClient) EnsureAlbum(ctx context.Context, name string) (app.RemoteAlbumID, error) {
	if ctx == nil {
		return "", errContextRequired
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("gdrive: ensure folder: %w", err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("gdrive: ensure folder: %w", app.ErrInvalidAlbum)
	}

	query := "'root' in parents and mimeType = '" + folderMimeType + "' and trashed = false and name = '" + escapeQueryLiteral(name) + "'"
	listed, err := c.service.Files.List().Q(query).PageSize(1).Fields("files(id)").Context(ctx).Do()
	if err != nil {
		return "", safeDriveError(ctx, "list folders", err)
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("gdrive: list folders: %w", err)
	}
	if listed != nil && len(listed.Files) > 0 {
		if listed.Files[0] == nil || strings.TrimSpace(listed.Files[0].Id) == "" {
			return "", errors.New("gdrive: list folders: Drive returned an empty folder ID")
		}
		return app.RemoteAlbumID(listed.Files[0].Id), nil
	}

	created, err := c.service.Files.Create(&drive.File{
		Name:     name,
		MimeType: folderMimeType,
		Parents:  []string{"root"},
	}).Fields("id").Context(ctx).Do()
	if err != nil {
		return "", safeDriveError(ctx, "create folder", err)
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("gdrive: create folder: %w", err)
	}
	if created == nil || strings.TrimSpace(created.Id) == "" {
		return "", errors.New("gdrive: create folder: Drive returned an empty folder ID")
	}
	return app.RemoteAlbumID(created.Id), nil
}

func escapeQueryLiteral(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `'`, `\'`)
}

func safeDriveError(ctx context.Context, operation string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("gdrive: %s: %w", operation, ctxErr)
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case 401:
			return fmt.Errorf("gdrive: %s: %w", operation, app.ErrAuthentication)
		case 429:
			return fmt.Errorf("gdrive: %s: %w", operation, app.ErrRateLimited)
		default:
			return fmt.Errorf("gdrive: %s: Drive API request failed (HTTP %d)", operation, apiErr.Code)
		}
	}
	return fmt.Errorf("gdrive: %s: Drive API request failed", operation)
}
