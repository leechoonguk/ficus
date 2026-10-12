package main

import (
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"ficus/internal/config"
	"ficus/internal/engine"
)

// popupAvailable reports whether the picker opens in a tmux popup:
// picker_popup is on, the shell runs inside tmux, and tmux is 3.2 or newer
// (display-popup -e and -d).
func popupAvailable(s config.Settings) bool {
	if !s.PickerPopup || os.Getenv("TMUX") == "" {
		return false
	}
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return false
	}
	m := regexp.MustCompile(`(\d+)\.(\d+)`).FindStringSubmatch(string(out))
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major > 3 || (major == 3 && minor >= 2)
}

// pickInPopup runs the picker in a tmux popup over the pane: ficus pick
// again, inside the popup, writing its result to a file that is copied to
// out. done is false when the popup could not run (the caller then picks
// inline); a cancelled pick is done with no output.
func pickInPopup(out io.Writer, sh engine.Shell, lbuffer string, tsv []byte, external bool) (done bool, err error) {
	exe, err := os.Executable()
	if err != nil {
		return false, nil
	}
	res, err := os.CreateTemp("", "ficus-pick-*")
	if err != nil {
		return false, nil
	}
	res.Close()
	defer os.Remove(res.Name())
	wd, _ := os.Getwd()

	args := []string{"display-popup", "-E", "-w", "80%", "-h", "60%", "-d", wd}
	// The popup runs in the tmux server's environment, not the shell's:
	// hand over the line and ficus's own settings.
	args = append(args, "-e", "FICUS_LBUFFER="+lbuffer)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "FICUS_") && !strings.HasPrefix(kv, "FICUS_LBUFFER=") {
			args = append(args, "-e", kv)
		}
	}
	args = append(args, "--", exe, "pick", "--shell", string(sh), "--fullscreen", "--result", res.Name())
	if external {
		f, err := os.CreateTemp("", "ficus-external-*")
		if err != nil {
			return false, nil
		}
		defer os.Remove(f.Name())
		_, werr := f.Write(tsv)
		if cerr := f.Close(); werr != nil || cerr != nil {
			return false, nil
		}
		args = append(args, "--external-file", f.Name())
	}
	runErr := exec.Command("tmux", args...).Run()
	data, _ := os.ReadFile(res.Name())
	if runErr != nil && len(data) == 0 {
		return false, nil
	}
	_, err = out.Write(data)
	return true, err
}
