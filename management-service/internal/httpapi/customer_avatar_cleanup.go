package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var uploadedAvatarSuffix = regexp.MustCompile(`^[0-9a-f]{16}\.(jpg|png|webp)$`)

func purgeCustomerAvatars(ctx context.Context, avatarDir string, userID int64) error {
	if userID <= 0 || strings.TrimSpace(avatarDir) == "" {
		return fmt.Errorf("invalid customer avatar scope")
	}
	root, err := os.OpenRoot(avatarDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	prefix := "user-" + strconv.FormatInt(userID, 10) + "-"
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		suffix, match := strings.CutPrefix(entry.Name(), prefix)
		if !match || !entry.Type().IsRegular() || !uploadedAvatarSuffix.MatchString(suffix) {
			continue
		}
		if err := root.Remove(entry.Name()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}
