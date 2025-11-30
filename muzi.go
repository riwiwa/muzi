package main

import "archive/zip"
import "path/filepath"
import "fmt"
import "strings"
import "os"
import "io"

func importSpotify() {
	path := filepath.Join(".", "spotify-data", "zip")
	targetBase := filepath.Join(".", "spotify-data", "extracted")
	entries, err := os.ReadDir(path)
	if err != nil {
		panic(err)
	}
	for _, f:= range entries {
		_, err := zip.OpenReader(filepath.Join(path, f.Name()))	
		if (err == nil) {
			fileName := f.Name()
			fileFullPath := filepath.Join(path, fileName)	
			fileBaseName := fileName[:(strings.LastIndex(fileName, "."))]
			targetDirFullPath := filepath.Join(targetBase, fileBaseName)

			extract(fileFullPath, targetDirFullPath)
		}
	}
}

func extract(path string, target string) {
	archive, err := zip.OpenReader(path)
	if (err != nil) {
		panic(err)
	}
	defer archive.Close()

	zipDir := filepath.Base(path)
	zipDir = zipDir[:(strings.LastIndex(zipDir, "."))]
	target = filepath.Join(target, zipDir)

	for _, f := range archive.File {
		filePath := filepath.Join(target, f.Name)
		fmt.Println("extracting:", filePath)
		
		if !strings.HasPrefix(filePath, filepath.Clean(target) + string(os.PathSeparator)) {
			fmt.Println("Invalid file path")
			return
		}
		if f.FileInfo().IsDir() {
			fmt.Println("Creating Directory", filePath)
			os.MkdirAll(filePath, os.ModePerm)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			panic(err)
		}
		fileToExtract, err := os.OpenFile(filePath, os.O_WRONLY | os.O_CREATE | os.O_TRUNC, f.Mode())
		if err != nil {
			panic(err)
		}
		extractedFile, err := f.Open()
		if err != nil {
			panic(err)
		}
		if _, err := io.Copy(fileToExtract, extractedFile); err != nil {
			panic(err)
		}
		fileToExtract.Close()
		extractedFile.Close()
	}
}

func main() {
}
