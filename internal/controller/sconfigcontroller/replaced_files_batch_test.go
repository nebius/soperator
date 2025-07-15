package sconfigcontroller

import (
	"os"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	fakes "nebius.ai/slurm-operator/internal/controller/sconfigcontroller/fake"
)

func prepareBatchTest(t *testing.T) (*ReplacedFilesBatch, *fakes.MockFs) {
	fakeFs := fakes.NewMockFs(t)
	batch := NewReplacedFilesBatch(fakeFs)
	return batch, fakeFs
}

func TestReplacedFilesBatch_HappyPathFilesPresent(t *testing.T) {
	batch, fakeFs := prepareBatchTest(t)

	fileName1 := "/test_file"
	tempFileName1 := "/test_file.tmp"
	content1 := []byte("test content")
	mode1 := os.FileMode(0o644)
	fileName2 := "/second_file"
	tempFileName2 := "/second_file.tmp"
	content2 := []byte("second content")
	mode2 := os.FileMode(0o600)

	fakeFs.
		On("MkdirAll", mock.AnythingOfType("string"), mock.AnythingOfType("FileMode")).
		Return(nil)
	fakeFs.
		On("PrepareNewFile", fileName1, mock.AnythingOfType("[]uint8"), mock.AnythingOfType("FileMode")).
		Return(tempFileName1, nil)
	fakeFs.
		On("PrepareNewFile", fileName2, mock.AnythingOfType("[]uint8"), mock.AnythingOfType("FileMode")).
		Return(tempFileName2, nil)
	fakeFs.
		On("RenameExchange", mock.AnythingOfType("string"), mock.AnythingOfType("string")).
		Return(nil)

	err := batch.Replace(fileName1, content1, mode1)
	require.NoError(t, err)

	err = batch.Replace(fileName2, content2, mode2)
	require.NoError(t, err)

	fakeFs.
		On("SyncCaches").
		Return(nil).
		// Should call SyncCaches once for several files
		Once()
	fakeFs.
		On("Remove", mock.AnythingOfType("string")).
		Return(nil)

	err = batch.Finish()
	require.NoError(t, err)

	// Nothing to clean

	err = batch.Cleanup()
	require.NoError(t, err)
}

func TestReplacedFilesBatch_HappyPathFilesMissing(t *testing.T) {
	batch, fakeFs := prepareBatchTest(t)

	fileName1 := "/test_file"
	tempFileName1 := "/test_file.tmp"
	content1 := []byte("test content")
	mode1 := os.FileMode(0o644)
	fileName2 := "/second_file"
	tempFileName2 := "/second_file.tmp"
	content2 := []byte("second content")
	mode2 := os.FileMode(0o600)

	fakeFs.
		On("MkdirAll", mock.AnythingOfType("string"), mock.AnythingOfType("FileMode")).
		Return(nil)
	fakeFs.
		On("PrepareNewFile", fileName1, mock.AnythingOfType("[]uint8"), mock.AnythingOfType("FileMode")).
		Return(tempFileName1, nil)
	fakeFs.
		On("PrepareNewFile", fileName2, mock.AnythingOfType("[]uint8"), mock.AnythingOfType("FileMode")).
		Return(tempFileName2, nil)
	fakeFs.
		On("RenameExchange", mock.AnythingOfType("string"), mock.AnythingOfType("string")).
		Return(os.ErrNotExist)
	fakeFs.
		On("RenameNoReplace", mock.AnythingOfType("string"), mock.AnythingOfType("string")).
		Return(nil)

	err := batch.Replace(fileName1, content1, mode1)
	require.NoError(t, err)

	err = batch.Replace(fileName2, content2, mode2)
	require.NoError(t, err)

	// Nothing to finish - files were not present

	err = batch.Finish()
	require.NoError(t, err)

	// Nothing to clean

	err = batch.Cleanup()
	require.NoError(t, err)
}

func TestReplacedFilesBatch_HappyPathFilesMixed(t *testing.T) {
	batch, fakeFs := prepareBatchTest(t)

	fileName1 := "/test_file"
	tempFileName1 := "/test_file.tmp"
	content1 := []byte("test content")
	mode1 := os.FileMode(0o644)
	fileName2 := "/second_file"
	tempFileName2 := "/second_file.tmp"
	content2 := []byte("second content")
	mode2 := os.FileMode(0o600)

	fakeFs.
		On("MkdirAll", mock.AnythingOfType("string"), mock.AnythingOfType("FileMode")).
		Return(nil)
	fakeFs.
		On("PrepareNewFile", fileName1, mock.AnythingOfType("[]uint8"), mock.AnythingOfType("FileMode")).
		Return(tempFileName1, nil)
	fakeFs.
		On("PrepareNewFile", fileName2, mock.AnythingOfType("[]uint8"), mock.AnythingOfType("FileMode")).
		Return(tempFileName2, nil)
	fakeFs.
		On("RenameExchange", tempFileName2, fileName2).
		Return(nil)
	fakeFs.
		On("RenameExchange", mock.AnythingOfType("string"), mock.AnythingOfType("string")).
		Return(os.ErrNotExist)
	fakeFs.
		On("RenameNoReplace", mock.AnythingOfType("string"), mock.AnythingOfType("string")).
		Return(nil)

	err := batch.Replace(fileName1, content1, mode1)
	require.NoError(t, err)

	err = batch.Replace(fileName2, content2, mode2)
	require.NoError(t, err)

	// Should call SyncCaches when at least one file was present
	fakeFs.
		On("SyncCaches").
		Return(nil).
		Once()
	fakeFs.
		On("Remove", mock.AnythingOfType("string")).
		Return(nil)

	err = batch.Finish()
	require.NoError(t, err)

	// Nothing to clean

	err = batch.Cleanup()
	require.NoError(t, err)
}

func TestReplacedFilesBatch_CleanupOne(t *testing.T) {
	batch, fakeFs := prepareBatchTest(t)

	fileName1 := "/test_file"
	tempFileName1 := "/test_file.tmp"
	content1 := []byte("test content")
	mode1 := os.FileMode(0o644)
	fileName2 := "/second_file"
	tempFileName2 := "/second_file.tmp"
	content2 := []byte("second content")
	mode2 := os.FileMode(0o600)

	fakeFs.
		On("MkdirAll", mock.AnythingOfType("string"), mock.AnythingOfType("FileMode")).
		Return(nil)
	fakeFs.
		On("PrepareNewFile", fileName1, mock.AnythingOfType("[]uint8"), mock.AnythingOfType("FileMode")).
		Return(tempFileName1, nil)
	// Preparing second files should fail
	fakeFs.
		On("PrepareNewFile", fileName2, mock.AnythingOfType("[]uint8"), mock.AnythingOfType("FileMode")).
		Return(tempFileName2, os.ErrPermission)
	fakeFs.
		On("RenameExchange", mock.AnythingOfType("string"), mock.AnythingOfType("string")).
		Return(nil).
		Once()

	err := batch.Replace(fileName1, content1, mode1)
	require.NoError(t, err)

	err = batch.Replace(fileName2, content2, mode2)
	require.ErrorContains(t, err, "preparing new file")
	require.ErrorContains(t, err, "permission denied")

	// Should rename first file back and remove temp file
	fakeFs.
		On("RenameExchange", tempFileName1, fileName1).
		Return(nil).
		Once()
	fakeFs.
		On("Remove", tempFileName1).
		Return(nil)

	err = batch.Cleanup()
	require.NoError(t, err)
}
