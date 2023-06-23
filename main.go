package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/go-openapi/runtime/logger"
	"github.com/gogo/status"
	backuperpb "github.com/vasiliy-grinko/s3-grpc-backuper/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"honnef.co/go/tools/config"
)

type FileServiceServer struct {
	backuperpb.UnimplementedFileServiceServer
	l   *logger.Logger
	cfg *config.Config
}

func (g *FileServiceServer) Upload(stream backuperpb.FileService_UploadServer) error {
	file := NewFile()
	var fileSize uint32
	fileSize = 0
	defer func() {
		if err := file.OutputFile.Close(); err != nil {
			g.l.Error(err)
		}
	}()
	for {
		req, err := stream.Recv()
		if file.FilePath == "" {
			file.SetFile(req.GetFileName(), g.cfg.FilesStorage.Location)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return g.logError(status.Error(codes.Internal, err.Error()))
		}
		chunk := req.GetChunk()
		fileSize += uint32(len(chunk))
		g.l.Debug("received a chunk with size: %d", fileSize)
		if err := file.Write(chunk); err != nil {
			return g.logError(status.Error(codes.Internal, err.Error()))
		}
	}
	fileName := filepath.Base(file.FilePath)
	g.l.Debug("saved file: %s, size: %d", fileName, fileSize)
	return stream.SendAndClose(&uploadpb.FileUploadResponse{FileName: fileName, Size: fileSize})
}

func NewFile() *File {
	return &File{
		buffer: &bytes.Buffer{},
	}
}

func New(l *logger.Logger, cfg *config.Config) *FileServiceServer {
	return &FileServiceServer{
		l:   l,
		cfg: cfg,
	}
}

func (g *FileServiceServer) logError(err error) error {
	if err != nil {
		g.l.Debug(err)
	}
	return err
}

func (g *FileServiceServer) contextError(ctx context.Context) error {
	switch ctx.Err() {
	case context.Canceled:
		return g.logError(status.Error(codes.Canceled, "request is canceled"))
	case context.DeadlineExceeded:
		return g.logError(status.Error(codes.DeadlineExceeded, "deadline is exceeded"))
	default:
		return nil
	}
}

type File struct {
	FilePath   string
	buffer     *bytes.Buffer
	OutputFile *os.File
}

func (f *File) SetFile(fileName, path string) error {
	err := os.MkdirAll(path, os.ModePerm)
	if err != nil {
		log.Fatal(err)
	}
	f.FilePath = filepath.Join(path, fileName)
	file, err := os.Create(f.FilePath)
	if err != nil {
		return err
	}
	f.OutputFile = file
	return nil
}

func (f *File) Write(chunk []byte) error {
	if f.OutputFile == nil {
		return nil
	}
	_, err := f.OutputFile.Write(chunk)
	return err
}

func (f *File) Close() error {
	return f.OutputFile.Close()
}

func main() {
	lis, err := net.Listen("tcp", ":9000")
	if err != nil {
		log.Fatalf("Failed to listen on port 9000: %v", err)
	}

	grpcServer := grpc.NewServer()
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to create gRPC server: %v", err)
	}
}
