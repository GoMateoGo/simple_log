package dailyxlog

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Config 定义完整的日志配置
type Config struct {
	Level      string // debug, info, warn, error
	Stdout     bool   // 是否输出到控制台
	Dir        string // 基础日志目录
	Filename   string // 基础日志文件名
	MaxSize    int    // 单个文件最大大小 (MB)
	MaxBackups int    // 最大旧文件保留个数
	MaxAge     int    // 最大保留天数
	Compress   bool   // 是否对旧日志压缩
}

var (
	globalSugar *zap.SugaredLogger
)

// New 全局初始化函数，供业务在启动时调用
func New(cfg Config) {
	// 1. 设置日志级别
	atomicLevel := zap.NewAtomicLevel()
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		level = zapcore.InfoLevel
	}
	atomicLevel.SetLevel(level)

	// 2. 配置 Encoder (格式化)
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")

	consoleEncoderConfig := zap.NewDevelopmentEncoderConfig()
	consoleEncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	consoleEncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")

	var cores []zapcore.Core

	// --- Core A: 文件输出 ---
	if cfg.Filename != "" {
		fileWriter := zapcore.AddSync(NewDailyLumberjack(cfg))
		cores = append(cores, zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderConfig),
			fileWriter,
			atomicLevel,
		))
	}

	// --- Core B: 控制台输出 ---
	if cfg.Stdout {
		cores = append(cores, zapcore.NewCore(
			zapcore.NewConsoleEncoder(consoleEncoderConfig),
			zapcore.Lock(os.Stdout),
			atomicLevel,
		))
	}

	core := zapcore.NewTee(cores...)

	// 3. 创建 Logger
	// AddCallerSkip(1) 是因为业务层如果直接通过 logx 代理包调用，需要跳过两层。
	// 但此处是作为基础库封装，这里默认跳过1层即可。如果您项目中还有二次封装，请自行基于 GetSugar() 调整。
	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	globalSugar = logger.Sugar()
}

// GetSugar 供外部获取原始的 SugaredLogger，主要为了实现特定功能如 WithContext
func GetSugar() *zap.SugaredLogger {
	return globalSugar
}

// DailyLumberjack 包装 lumberjack.Logger，实现按日期生成目录+文件尺寸切割双维度
type DailyLumberjack struct {
	mu  sync.RWMutex
	cfg Config

	currentYear int
	currentDay  int
	logger      *lumberjack.Logger
}

// NewDailyLumberjack 创建一个新的 DailyLumberjack 实例
func NewDailyLumberjack(cfg Config) *DailyLumberjack {
	return &DailyLumberjack{
		cfg: cfg,
	}
}

func (l *DailyLumberjack) getLogger() *lumberjack.Logger {
	now := time.Now()
	nowYear, nowDay := now.Year(), now.YearDay()

	l.mu.RLock()
	if l.currentYear == nowYear && l.currentDay == nowDay && l.logger != nil {
		logger := l.logger
		l.mu.RUnlock()
		return logger
	}
	l.mu.RUnlock()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Double check
	if l.currentYear == nowYear && l.currentDay == nowDay && l.logger != nil {
		return l.logger
	}

	if l.logger != nil {
		_ = l.logger.Close()
	}

	l.currentYear = nowYear
	l.currentDay = nowDay
	nowDate := now.Format("2006-01-02")
	dailyFilename := filepath.Join(l.cfg.Dir, nowDate, l.cfg.Filename)
	_ = os.MkdirAll(filepath.Dir(dailyFilename), 0755)

	// 异步清理过期日志目录，实现真正的 MaxAge
	go l.cleanOldDirs(now)

	l.logger = &lumberjack.Logger{
		Filename:   dailyFilename,
		MaxSize:    l.cfg.MaxSize,
		MaxBackups: l.cfg.MaxBackups,
		MaxAge:     l.cfg.MaxAge,
		Compress:   l.cfg.Compress,
	}
	return l.logger
}

func (l *DailyLumberjack) Write(p []byte) (n int, err error) {
	return l.getLogger().Write(p)
}

func (l *DailyLumberjack) Sync() error {
	return nil
}

func (l *DailyLumberjack) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.logger != nil {
		return l.logger.Close()
	}
	return nil
}

// -------------------------------------------------------
// 以下是包级别的快捷函数，支持 Printf 风格的占位符
// -------------------------------------------------------

func Debug(template string, args ...interface{}) {
	if globalSugar != nil {
		globalSugar.Debugf(template, args...)
	}
}

func Info(template string, args ...interface{}) {
	if globalSugar != nil {
		globalSugar.Infof(template, args...)
	}
}

func Warn(template string, args ...interface{}) {
	if globalSugar != nil {
		globalSugar.Warnf(template, args...)
	}
}

func Error(template string, args ...interface{}) {
	if globalSugar != nil {
		globalSugar.Errorf(template, args...)
	}
}

func Fatal(template string, args ...interface{}) {
	if globalSugar != nil {
		globalSugar.Fatalf(template, args...)
	}
}

// -------------------------------------------------------
// 以下是结构化日志方法 (Structured Logging)
// -------------------------------------------------------

func Debugw(msg string, keysAndValues ...interface{}) {
	if globalSugar != nil {
		globalSugar.Debugw(msg, keysAndValues...)
	}
}

func Infow(msg string, keysAndValues ...interface{}) {
	if globalSugar != nil {
		globalSugar.Infow(msg, keysAndValues...)
	}
}

func Warnw(msg string, keysAndValues ...interface{}) {
	if globalSugar != nil {
		globalSugar.Warnw(msg, keysAndValues...)
	}
}

func Errorw(msg string, keysAndValues ...interface{}) {
	if globalSugar != nil {
		globalSugar.Errorw(msg, keysAndValues...)
	}
}

func Fatalw(msg string, keysAndValues ...interface{}) {
	if globalSugar != nil {
		globalSugar.Fatalw(msg, keysAndValues...)
	}
}

// cleanOldDirs 扫描日志根目录，清理超出 MaxAge 的日期文件夹
func (l *DailyLumberjack) cleanOldDirs(now time.Time) {
	if l.cfg.MaxAge <= 0 {
		return
	}

	entries, err := os.ReadDir(l.cfg.Dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirTime, err := time.Parse("2006-01-02", entry.Name())
		if err != nil {
			continue // 非目标日期格式文件夹，跳过
		}

		daysOld := int(now.Sub(dirTime).Hours() / 24)
		if daysOld > l.cfg.MaxAge {
			_ = os.RemoveAll(filepath.Join(l.cfg.Dir, entry.Name()))
		}
	}
}
