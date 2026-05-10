package prepare

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"time"
)

// runHeaderPrefix marks the start of one bay's run within a shared dock log.
// FilterRunsForBay relies on this to know where to start/stop including lines.
const runHeaderPrefix = "==== run "

// writeRunHeader emits the per-run banner that FilterRunsForBay scans for.
func writeRunHeader(w io.Writer, bayID, stepName string, ts time.Time) error {
	_, err := fmt.Fprintf(w, "%sbay=%s step=%s %s ====\n", runHeaderPrefix, bayID, stepName, ts.Format(time.RFC3339))
	return err
}

// FilterRunsForBay copies lines from r to w, keeping only the runs whose
// header names bayID. An empty bayID copies everything.
func FilterRunsForBay(w io.Writer, r io.Reader, bayID string) error {
	if bayID == "" {
		_, err := io.Copy(w, r)
		return err
	}
	headerPrefix := []byte(runHeaderPrefix)
	marker := []byte(" bay=" + bayID + " ")
	include := false
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 && err == io.EOF {
			return nil
		}
		if err != nil && err != io.EOF {
			return err
		}
		if bytes.HasPrefix(line, headerPrefix) {
			include = bytes.Contains(line, marker)
		}
		if include {
			if _, writeErr := w.Write(line); writeErr != nil {
				return writeErr
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}

// FilterRunsForBayFile streams path through FilterRunsForBay. A missing file
// is treated as empty so CLI callers don't have to special-case "no log yet".
func FilterRunsForBayFile(w io.Writer, path, bayID string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	return FilterRunsForBay(w, f, bayID)
}
