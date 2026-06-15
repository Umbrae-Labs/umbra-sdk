package umbra

import (
	"context"
	"os/exec"
	"runtime"
)

type BrowserOpener interface {
	OpenURL(ctx context.Context, url string) error
}

type SystemBrowserOpener struct{}

func (SystemBrowserOpener) OpenURL(ctx context.Context, target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", target)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", target)
	}
	return cmd.Start()
}

type NoopBrowserOpener struct{}

func (NoopBrowserOpener) OpenURL(context.Context, string) error {
	return nil
}
