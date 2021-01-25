package lumberjack

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v2"
)

// !!!NOTE!!!
//
// Running these tests in parallel will almost certainly cause sporadic (or even
// regular) failures, because they're all messing with the same global variable
// that controls the logic's mocked time.Now.  So... don't do that.

// Since all the tests uses the time to determine filenames etc, we need to
// control the wall clock as much as possible, which means having a wall clock
// that doesn't change unless we want it to.
var fakeCurrentTime = time.Now()

func fakeTime() time.Time {
	return fakeCurrentTime
}

func TestNewFile(t *testing.T) {
	currentTime = fakeTime

	dir := makeTempDir("TestNewFile", t)
	defer os.RemoveAll(dir)
	l := &Logger{
		Filename: logFile(dir),
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)
	existsWithContent(logFile(dir), b, t)
	fileCount(dir, 1, t)
}

func TestOpenExisting(t *testing.T) {
	currentTime = fakeTime
	dir := makeTempDir("TestOpenExisting", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	data := []byte("foo!")
	err := ioutil.WriteFile(filename, data, 0644)
	isNil(err, t)
	existsWithContent(filename, data, t)

	l := &Logger{
		Filename: filename,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	// make sure the file got appended
	existsWithContent(filename, append(data, b...), t)

	// make sure no other files were created
	fileCount(dir, 1, t)
}

func TestWriteTooLong(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1
	dir := makeTempDir("TestWriteTooLong", t)
	defer os.RemoveAll(dir)
	l := &Logger{
		Filename: logFile(dir),
		MaxSize:  5,
	}
	defer l.Close()
	b := []byte("booooooooooooooo!")
	n, err := l.Write(b)
	notNil(err, t)
	equals(0, n, t)
	equals(err.Error(),
		fmt.Sprintf("write length %d exceeds maximum file size %d", len(b), l.MaxSize), t)
	_, err = os.Stat(logFile(dir))
	assert(os.IsNotExist(err), t, "File exists, but should not have been created")
}

func TestMakeLogDir(t *testing.T) {
	currentTime = fakeTime
	dir := time.Now().Format("TestMakeLogDir" + DefaultTimeFormat)
	dir = filepath.Join(os.TempDir(), dir)
	defer os.RemoveAll(dir)
	filename := logFile(dir)
	l := &Logger{
		Filename: filename,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)
	existsWithContent(logFile(dir), b, t)
	fileCount(dir, 1, t)
}

func TestDefaultFilename(t *testing.T) {
	currentTime = fakeTime
	dir := os.TempDir()
	filename := filepath.Join(dir, filepath.Base(os.Args[0])+"-lumberjack.log")
	defer os.Remove(filename)
	l := &Logger{}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)

	isNil(err, t)
	equals(len(b), n, t)
	existsWithContent(filename, b, t)
}

func TestAutoRotate(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1

	dir := makeTempDir("TestAutoRotate", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	l := &Logger{
		Filename: filename,
		MaxSize:  10,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	existsWithContent(filename, b, t)
	fileCount(dir, 1, t)

	newFakeTime()

	b2 := []byte("foooooo!")
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// the old logfile should be moved aside and the main logfile should have
	// only the last write in it.
	existsWithContent(filename, b2, t)

	// the backup file will use the current fake time and have the old contents.
	existsWithContent(backupFile(dir), b, t)

	fileCount(dir, 2, t)
}

func TestFirstWriteRotate(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1
	dir := makeTempDir("TestFirstWriteRotate", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	l := &Logger{
		Filename: filename,
		MaxSize:  10,
	}
	defer l.Close()

	start := []byte("boooooo!")
	err := ioutil.WriteFile(filename, start, 0600)
	isNil(err, t)

	newFakeTime()

	// this would make us rotate
	b := []byte("fooo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	existsWithContent(filename, b, t)
	existsWithContent(backupFile(dir), start, t)

	fileCount(dir, 2, t)
}

func TestMaxBackups(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1
	dir := makeTempDir("TestMaxBackups", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	l := &Logger{
		Filename:   filename,
		MaxSize:    10,
		MaxBackups: 1,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	existsWithContent(filename, b, t)
	fileCount(dir, 1, t)

	newFakeTime()

	// this will put us over the max
	b2 := []byte("foooooo!")
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// this will use the new fake time
	secondFilename := backupFile(dir)
	existsWithContent(secondFilename, b, t)

	// make sure the old file still exists with the same content.
	existsWithContent(filename, b2, t)

	fileCount(dir, 2, t)

	newFakeTime()

	// this will make us rotate again
	b3 := []byte("baaaaaar!")
	n, err = l.Write(b3)
	isNil(err, t)
	equals(len(b3), n, t)

	// this will use the new fake time
	thirdFilename := backupFile(dir)
	existsWithContent(thirdFilename, b2, t)

	existsWithContent(filename, b3, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(time.Millisecond * 10)

	// should only have two files in the dir still
	fileCount(dir, 2, t)

	// second file name should still exist
	existsWithContent(thirdFilename, b2, t)

	// should have deleted the first backup
	notExist(secondFilename, t)

	// now test that we don't delete directories or non-logfile files

	newFakeTime()

	// create a file that is close to but different from the logfile name.
	// It shouldn't get caught by our deletion filters.
	notlogfile := logFile(dir) + ".foo"
	err = ioutil.WriteFile(notlogfile, []byte("data"), 0644)
	isNil(err, t)

	// Make a directory that exactly matches our log file filters... it still
	// shouldn't get caught by the deletion filter since it's a directory.
	notlogfiledir := backupFile(dir)
	err = os.Mkdir(notlogfiledir, 0700)
	isNil(err, t)

	newFakeTime()

	// this will use the new fake time
	fourthFilename := backupFile(dir)

	// Create a log file that is/was being compressed - this should
	// not be counted since both the compressed and the uncompressed
	// log files still exist.
	compLogFile := fourthFilename + compressSuffix
	err = ioutil.WriteFile(compLogFile, []byte("compress"), 0644)
	isNil(err, t)

	// this will make us rotate again
	b4 := []byte("baaaaaaz!")
	n, err = l.Write(b4)
	isNil(err, t)
	equals(len(b4), n, t)

	existsWithContent(fourthFilename, b3, t)
	existsWithContent(fourthFilename+compressSuffix, []byte("compress"), t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(time.Millisecond * 10)

	// We should have four things in the directory now - the 2 log files, the
	// not log file, and the directory
	fileCount(dir, 5, t)

	// third file name should still exist
	existsWithContent(filename, b4, t)

	existsWithContent(fourthFilename, b3, t)

	// should have deleted the first filename
	notExist(thirdFilename, t)

	// the not-a-logfile should still exist
	exists(notlogfile, t)

	// the directory
	exists(notlogfiledir, t)
}

func TestCleanupExistingBackups(t *testing.T) {
	// test that if we start with more backup files than we're supposed to have
	// in total, that extra ones get cleaned up when we rotate.

	currentTime = fakeTime
	megabyte = 1

	dir := makeTempDir("TestCleanupExistingBackups", t)
	defer os.RemoveAll(dir)

	// make 3 backup files

	data := []byte("data")
	backup := backupFile(dir)
	err := ioutil.WriteFile(backup, data, 0644)
	isNil(err, t)

	newFakeTime()

	backup = backupFile(dir)
	err = ioutil.WriteFile(backup+compressSuffix, data, 0644)
	isNil(err, t)

	newFakeTime()

	backup = backupFile(dir)
	err = ioutil.WriteFile(backup, data, 0644)
	isNil(err, t)

	// now create a primary log file with some data
	filename := logFile(dir)
	err = ioutil.WriteFile(filename, data, 0644)
	isNil(err, t)

	l := &Logger{
		Filename:   filename,
		MaxSize:    10,
		MaxBackups: 1,
	}
	defer l.Close()

	newFakeTime()

	b2 := []byte("foooooo!")
	n, err := l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(time.Millisecond * 10)

	// now we should only have 2 files left - the primary and one backup
	fileCount(dir, 2, t)
}

func TestMaxAge(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1

	dir := makeTempDir("TestMaxAge", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	l := &Logger{
		Filename: filename,
		MaxSize:  10,
		MaxAge:   1,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	existsWithContent(filename, b, t)
	fileCount(dir, 1, t)

	// two days later
	newFakeTime()

	b2 := []byte("foooooo!")
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)
	existsWithContent(backupFile(dir), b, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(10 * time.Millisecond)

	// We should still have 2 log files, since the most recent backup was just
	// created.
	fileCount(dir, 2, t)

	existsWithContent(filename, b2, t)

	// we should have deleted the old file due to being too old
	existsWithContent(backupFile(dir), b, t)

	// two days later
	newFakeTime()

	b3 := []byte("baaaaar!")
	n, err = l.Write(b3)
	isNil(err, t)
	equals(len(b3), n, t)
	existsWithContent(backupFile(dir), b2, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(10 * time.Millisecond)

	// We should have 2 log files - the main log file, and the most recent
	// backup.  The earlier backup is past the cutoff and should be gone.
	fileCount(dir, 2, t)

	existsWithContent(filename, b3, t)

	// we should have deleted the old file due to being too old
	existsWithContent(backupFile(dir), b2, t)
}

func TestOldLogFiles(t *testing.T) {
	forEachBackupTestSpec(t, func(t *testing.T, test backupTestSpec) {
		currentTime = fakeTime
		megabyte = 1

		dir := makeTempDir("TestOldLogFiles", t)
		defer os.RemoveAll(dir)
		var backupDir string
		effectiveBackupDir := dir
		if test.customBackupDir {
			backupDir = makeTempDir("TestOldLogFilesBackup", t)
			defer os.RemoveAll(backupDir)
			effectiveBackupDir = backupDir
		}

		filename := logFile(dir)
		data := []byte("data")
		err := ioutil.WriteFile(filename, data, 07)
		isNil(err, t)

		// This gives us a time with the same precision as the time we get from the
		// timestamp in the name.
		getTime := func() time.Time {
			theTime := fakeTime()
			if !test.local {
				theTime = theTime.UTC()
			}
			theTime, err := time.Parse(test.timeFormat, theTime.Format(test.timeFormat))
			isNil(err, t)
			return theTime
		}

		t1 := getTime()

		backup := backupFile(effectiveBackupDir, withLocalTime(test.local), withTimeFormat(test.timeFormat))
		err = ioutil.WriteFile(backup, data, 07)
		isNil(err, t)

		newFakeTime()

		t2 := getTime()

		backup2 := backupFile(effectiveBackupDir, withLocalTime(test.local), withTimeFormat(test.timeFormat))
		err = ioutil.WriteFile(backup2, data, 07)
		isNil(err, t)

		l := &Logger{Filename: filename, LocalTime: test.local, TimeFormat: test.timeFormat, BackupDir: backupDir}
		files, err := l.oldLogFiles()
		isNil(err, t)
		equals(2, len(files), t)

		// should be sorted by newest file first, which would be t2
		equals(t2, files[0].timestamp, t)
		equals(t1, files[1].timestamp, t)
	})
}

func TestTimeFromName(t *testing.T) {
	l := &Logger{Filename: "/var/log/myfoo/foo.log"}
	prefix, ext := l.prefixAndExt()

	tests := []struct {
		filename string
		want     time.Time
		wantErr  bool
	}{
		{"foo-2014-05-04T14-44-33.555.log", time.Date(2014, 5, 4, 14, 44, 33, 555000000, time.UTC), false},
		{"foo-2014-05-04T14-44-33.555", time.Time{}, true},
		{"2014-05-04T14-44-33.555.log", time.Time{}, true},
		{"foo.log", time.Time{}, true},
	}

	for _, test := range tests {
		got, err := l.timeFromName(test.filename, prefix, ext)
		equals(got, test.want, t)
		equals(err != nil, test.wantErr, t)
	}
}

func TestLocalTime(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1

	dir := makeTempDir("TestLocalTime", t)
	defer os.RemoveAll(dir)

	l := &Logger{
		Filename:  logFile(dir),
		MaxSize:   10,
		LocalTime: true,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	b2 := []byte("fooooooo!")
	n2, err := l.Write(b2)
	isNil(err, t)
	equals(len(b2), n2, t)

	existsWithContent(logFile(dir), b2, t)
	existsWithContent(backupFile(dir, withLocalTime(true)), b, t)
}

func TestRotate(t *testing.T) {
	forEachBackupTestSpec(t, func(t *testing.T, test backupTestSpec) {
		currentTime = fakeTime
		dir := makeTempDir("TestRotate", t)
		defer os.RemoveAll(dir)
		var backupDir string
		effectiveBackupDir := dir
		if test.customBackupDir {
			// Temp non-existing dir - expected to be created on rotate
			backupDir = filepath.Join(makeTempDir("TestOldLogFilesBackup", t), "backups")
			defer os.RemoveAll(backupDir)
			effectiveBackupDir = backupDir
		}

		filename := logFile(dir)

		l := &Logger{
			Filename:   filename,
			MaxBackups: 1,
			MaxSize:    100, // megabytes
			BackupDir:  backupDir,
			TimeFormat: test.timeFormat,
			LocalTime:  test.local,
		}
		defer l.Close()
		b := []byte("boo!")
		n, err := l.Write(b)
		isNil(err, t)
		equals(len(b), n, t)

		existsWithContent(filename, b, t)
		fileCount(dir, 1, t)

		newFakeTime()

		err = l.Rotate()
		isNil(err, t)

		// we need to wait a little bit since the files get deleted on a different
		// goroutine.
		<-time.After(10 * time.Millisecond)

		filename2 := backupFile(effectiveBackupDir, withLocalTime(test.local), withTimeFormat(test.timeFormat))
		existsWithContent(filename2, b, t)
		existsWithContent(filename, []byte{}, t)
		if test.customBackupDir {
			fileCount(dir, 1, t)
			fileCount(effectiveBackupDir, 1, t)
		} else {
			fileCount(dir, 2, t)
		}
		newFakeTime()

		err = l.Rotate()
		isNil(err, t)

		// we need to wait a little bit since the files get deleted on a different
		// goroutine.
		<-time.After(10 * time.Millisecond)

		filename3 := backupFile(effectiveBackupDir, withLocalTime(test.local), withTimeFormat(test.timeFormat))
		existsWithContent(filename3, []byte{}, t)
		existsWithContent(filename, []byte{}, t)
		if test.customBackupDir {
			fileCount(dir, 1, t)
			fileCount(effectiveBackupDir, 1, t)
		} else {
			fileCount(dir, 2, t)
		}

		b2 := []byte("foooooo!")
		n, err = l.Write(b2)
		isNil(err, t)
		equals(len(b2), n, t)

		// this will use the new fake time
		existsWithContent(filename, b2, t)
	})
}

func TestCompressOnRotate(t *testing.T) {
	tests := []struct {
		name                 string
		keepLastDecompressed int
		verifyFirst          func(string, []byte, testing.TB)
		verifySecond         func(string, []byte, testing.TB)
	}{
		{
			name:                 "compress all",
			keepLastDecompressed: 0,
			verifyFirst:          verifyCompressedFile,
			verifySecond:         verifyCompressedFile,
		},
		{
			name:                 "keep 1 decompressed",
			keepLastDecompressed: 1,
			verifyFirst:          verifyCompressedFile,
			verifySecond:         existsWithContent,
		},
		{
			name:                 "keep 2 decompressed",
			keepLastDecompressed: 2,
			verifyFirst:          existsWithContent,
			verifySecond:         existsWithContent,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currentTime = fakeTime
			megabyte = 1

			dir := makeTempDir("TestCompressOnRotate", t)
			defer func() { _ = os.RemoveAll(dir) }()

			logFilename := logFile(dir)
			l := &Logger{
				Compress:             true,
				KeepLastDecompressed: test.keepLastDecompressed,
				Filename:             logFilename,
				MaxSize:              10,
			}
			defer l.Close()
			booBytes := []byte("boo!")
			writeToCurrentLog(t, l, logFilename, booBytes)

			fileCount(dir, 1, t)

			newFakeTime()
			firstArchiveTime := fakeTime()

			err := l.Rotate()
			isNil(err, t)

			// the old logfile should be moved aside and the main logfile should have
			// nothing in it.
			oldLogFilename := backupFileWithTime(dir, firstArchiveTime)
			existsWithContent(oldLogFilename, booBytes, t)
			existsWithContent(logFilename, []byte{}, t)

			haaBytes := []byte("haaa!")
			writeToCurrentLog(t, l, logFilename, haaBytes)

			newFakeTime()
			secondArchiveTime := fakeTime()

			err = l.Rotate()
			isNil(err, t)
			// we need to wait a little bit since the files get compressed on a different
			// goroutine.
			<-time.After(300 * time.Millisecond)

			test.verifyFirst(backupFileWithTime(dir, firstArchiveTime), booBytes, t)
			test.verifySecond(backupFileWithTime(dir, secondArchiveTime), haaBytes, t)

			fileCount(dir, 3, t)
		})
	}
}

func TestCompressOnResume(t *testing.T) {
	tests := []struct {
		name                 string
		keepLastDecompressed int
		expectedFileCount    int
	}{
		{
			name:                 "compress latest",
			keepLastDecompressed: 0,
			expectedFileCount:    2,
		},
		{
			name:                 "don't compress latest",
			keepLastDecompressed: 1,
			expectedFileCount:    3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currentTime = fakeTime
			megabyte = 1

			dir := makeTempDir("TestCompressOnResume", t)
			defer os.RemoveAll(dir)

			filename := logFile(dir)
			l := &Logger{
				Compress:             true,
				KeepLastDecompressed: test.keepLastDecompressed,
				Filename:             filename,
				MaxSize:              10,
			}
			defer l.Close()

			t1 := fakeTime()
			// Create a backup file and empty "compressed" file.
			previouslyArchivedFile := backupFileWithTime(dir, t1)
			fooBytes := []byte("foo!")
			err := ioutil.WriteFile(previouslyArchivedFile, fooBytes, 0644)
			isNil(err, t)
			err = ioutil.WriteFile(previouslyArchivedFile+compressSuffix, []byte{}, 0644)
			isNil(err, t)

			writeToCurrentLog(t, l, filename, []byte("boo!"))
			newFakeTime()

			if test.keepLastDecompressed > 0 {
				// in this case another backup file is needed
				writeToCurrentLog(t, l, filename, []byte("haaaaa!"))
				newFakeTime()
			}

			// we need to wait a little bit since the files get compressed on a different
			// goroutine.
			<-time.After(300 * time.Millisecond)

			verifyCompressedFile(previouslyArchivedFile, fooBytes, t)
			fileCount(dir, test.expectedFileCount, t)
		})
	}
}

func verifyCompressedFile(archivedFilename string, contents []byte, t testing.TB) {
	// The write should have started the compression - a compressed version of
	// the log file should now exist and the original should have been removed.
	bc := new(bytes.Buffer)
	gz := gzip.NewWriter(bc)
	_, err := gz.Write(contents)
	isNil(err, t)
	err = gz.Close()
	isNil(err, t)
	existsWithContent(archivedFilename+compressSuffix, bc.Bytes(), t)
	notExist(archivedFilename, t)
}

func writeToCurrentLog(t *testing.T, l *Logger, filename string, contents []byte) {
	b := contents
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)
	existsWithContent(filename, b, t)
}

func TestJson(t *testing.T) {
	data := []byte(`
{
	"filename": "foo",
	"maxsize": 5,
	"maxage": 10,
	"maxbackups": 3,
	"localtime": true,
	"compress": true,
	"keeplastdecompressed": 2,
	"timeformat": "1:2.3",
	"backupdir": "bar"
}`[1:])

	l := Logger{}
	err := json.Unmarshal(data, &l)
	isNil(err, t)
	equals("foo", l.Filename, t)
	equals(5, l.MaxSize, t)
	equals(10, l.MaxAge, t)
	equals(3, l.MaxBackups, t)
	equals(true, l.LocalTime, t)
	equals(true, l.Compress, t)
	equals(2, l.KeepLastDecompressed, t)
	equals("1:2.3", l.TimeFormat, t)
	equals("bar", l.BackupDir, t)
}

func TestYaml(t *testing.T) {
	data := []byte(`
filename: foo
maxsize: 5
maxage: 10
maxbackups: 3
localtime: true
compress: true
keeplastdecompressed: 2
timeformat: 1:2.3
backupdir: bar`[1:])

	l := Logger{}
	err := yaml.Unmarshal(data, &l)
	isNil(err, t)
	equals("foo", l.Filename, t)
	equals(5, l.MaxSize, t)
	equals(10, l.MaxAge, t)
	equals(3, l.MaxBackups, t)
	equals(true, l.LocalTime, t)
	equals(true, l.Compress, t)
	equals(2, l.KeepLastDecompressed, t)
	equals("1:2.3", l.TimeFormat, t)
	equals("bar", l.BackupDir, t)
}

func TestToml(t *testing.T) {
	data := `
filename = "foo"
maxsize = 5
maxage = 10
maxbackups = 3
localtime = true
compress = true
keeplastdecompressed = 2
timeformat = "1:2.3"
backupdir = "bar"`[1:]

	l := Logger{}
	md, err := toml.Decode(data, &l)
	isNil(err, t)
	equals("foo", l.Filename, t)
	equals(5, l.MaxSize, t)
	equals(10, l.MaxAge, t)
	equals(3, l.MaxBackups, t)
	equals(true, l.LocalTime, t)
	equals(true, l.Compress, t)
	equals(2, l.KeepLastDecompressed, t)
	equals("1:2.3", l.TimeFormat, t)
	equals("bar", l.BackupDir, t)
	equals(0, len(md.Undecoded()), t)
}

func TestShouldCompressFile(t *testing.T) {
	tests := []struct {
		name                 string
		keepLastDecompressed int
		filename             string
		fileIndices          []int
		expected             []bool
	}{
		{
			name:                 "compress all",
			filename:             "foo.log",
			fileIndices:          []int{0, 1, 2, 3},
			keepLastDecompressed: 0,
			expected:             []bool{true, true, true, true},
		},
		{
			name:                 "leave 2 decompressed",
			filename:             "foo.log",
			fileIndices:          []int{0, 1, 2, 3},
			keepLastDecompressed: 2,
			expected:             []bool{false, false, true, true},
		},
		{
			name:                 "leave 5 decompressed",
			filename:             "foo.log",
			fileIndices:          []int{0, 1, 2, 3},
			keepLastDecompressed: 5,
			expected:             []bool{false, false, false, false},
		},
		{
			name:                 "file already compressed",
			filename:             "foo.log.gz",
			fileIndices:          []int{0, 1, 2, 3},
			keepLastDecompressed: 0,
			expected:             []bool{false, false, false, false},
		},
	}

	for _, test := range tests {
		for _, i := range test.fileIndices {
			equals(test.expected[i], shouldCompressFile(test.keepLastDecompressed, i, test.filename), t)
		}
	}

}

func forEachBackupTestSpec(t *testing.T, do func(t *testing.T, test backupTestSpec)) {
	for _, test := range backupTestSpecs() {
		t.Run(test.name, func(t *testing.T) {
			do(t, test)
		})
	}
}

type backupTestSpec struct {
	name            string
	local           bool
	timeFormat      string
	customBackupDir bool
}

func backupTestSpecs() []backupTestSpec {
	return []backupTestSpec{
		{
			name:            "Default time format, UTC, default backup dir",
			local:           false,
			timeFormat:      DefaultTimeFormat,
			customBackupDir: false,
		},
		{
			name:            "Default time format, local time, custom backup dir",
			local:           true,
			timeFormat:      DefaultTimeFormat,
			customBackupDir: true,
		},
		{
			name:            "Custom time format, UTC, custom backup dir",
			local:           false,
			timeFormat:      "20060102150405000",
			customBackupDir: true,
		},
		{
			name:            "Default time format, local time, default backup dir",
			local:           true,
			timeFormat:      "2006.01.02.15.04.05.000",
			customBackupDir: false,
		},
	}
}

// makeTempDir creates a file with a semi-unique name in the OS temp directory.
// It should be based on the name of the test, to keep parallel tests from
// colliding, and must be cleaned up after the test is finished.
func makeTempDir(name string, t testing.TB) string {
	dir := time.Now().Format(name + DefaultTimeFormat)
	dir = filepath.Join(os.TempDir(), dir)
	isNilUp(os.Mkdir(dir, 0700), t, 1)
	return dir
}

// existsWithContent checks that the given file exists and has the correct content.
func existsWithContent(path string, content []byte, t testing.TB) {
	info, err := os.Stat(path)
	isNilUp(err, t, 1)
	equalsUp(int64(len(content)), info.Size(), t, 1)

	b, err := ioutil.ReadFile(path)
	isNilUp(err, t, 1)
	equalsUp(content, b, t, 1)
}

// logFile returns the log file name in the given directory for the current fake
// time.
func logFile(dir string) string {
	return filepath.Join(dir, "foobar.log")
}

func backupFile(dir string, opts ...backupFileOpt) string {
	return backupFileWithTime(dir, fakeTime(), opts...)
}

func backupFileWithTime(dir string, currTime time.Time, opts ...backupFileOpt) string {
	options := backupFileOpts{
		local:      false,
		timeFormat: DefaultTimeFormat,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if !options.local {
		currTime = currTime.UTC()
	}
	return filepath.Join(dir, "foobar-"+currTime.Format(options.timeFormat)+".log")
}

type backupFileOpts struct {
	local      bool
	timeFormat string
}

type backupFileOpt func(opts *backupFileOpts)

func withLocalTime(local bool) backupFileOpt {
	return func(opts *backupFileOpts) {
		opts.local = local
	}
}

func withTimeFormat(format string) backupFileOpt {
	return func(opts *backupFileOpts) {
		opts.timeFormat = format
	}
}

// fileCount checks that the number of files in the directory is exp.
func fileCount(dir string, exp int, t testing.TB) {
	files, err := ioutil.ReadDir(dir)
	isNilUp(err, t, 1)
	// Make sure no other files were created.
	equalsUp(exp, len(files), t, 1)
}

// newFakeTime sets the fake "current time" to two days later.
func newFakeTime() {
	fakeCurrentTime = fakeCurrentTime.Add(time.Hour * 24 * 2)
}

func notExist(path string, t testing.TB) {
	_, err := os.Stat(path)
	assertUp(os.IsNotExist(err), t, 1, "expected to get os.IsNotExist, but instead got %v", err)
}

func exists(path string, t testing.TB) {
	_, err := os.Stat(path)
	assertUp(err == nil, t, 1, "expected file to exist, but got error from os.Stat: %v", err)
}
