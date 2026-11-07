package main

// Windows does not support fsync on a directory handle opened by os.Open.
// The temporary file is flushed before os.Rename.
func syncDirectory(path string) error { return nil }
