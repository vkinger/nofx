package store

import (
	"time"

	"gorm.io/gorm"
)

// UserStore user storage
type UserStore struct {
	db *gorm.DB
}

// User user model
type User struct {
	ID             string    `gorm:"primaryKey" json:"id"`
	Email          string    `gorm:"uniqueIndex:idx_users_email;not null" json:"email"`
	PasswordHash   string    `gorm:"column:password_hash;not null" json:"-"`
	OTPSecret      string    `gorm:"column:otp_secret" json:"-"`
	OTPVerified    bool      `gorm:"column:otp_verified;default:false" json:"otp_verified"`
	TelegramChatID int64     `gorm:"column:telegram_chat_id;default:0;index" json:"telegram_chat_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (User) TableName() string { return "users" }

// NewUserStore creates a new UserStore
func NewUserStore(db *gorm.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) initTables() error {
	// For PostgreSQL with existing table, skip AutoMigrate to avoid index conflicts
	if s.db.Dialector.Name() == "postgres" {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'users'`).Scan(&tableExists)

		if tableExists > 0 {
			// Table exists - manually ensure all columns exist
			// Core columns (should already exist)
			s.db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT ''`)
			s.db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT NOT NULL DEFAULT ''`)
			s.db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP`)
			s.db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP`)
			// OTP columns (added later)
			s.db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS otp_secret TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS otp_verified BOOLEAN DEFAULT FALSE`)
			// Telegram Chat ID column
			s.db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS telegram_chat_id BIGINT DEFAULT 0`)

			// Create index for telegram_chat_id if not exists
			var telegramIndexExists int64
			s.db.Raw(`
				SELECT COUNT(*) FROM pg_indexes
				WHERE tablename = 'users' AND indexdef LIKE '%telegram_chat_id%'
			`).Scan(&telegramIndexExists)
			if telegramIndexExists == 0 {
				s.db.Exec("CREATE INDEX IF NOT EXISTS idx_users_telegram_chat_id ON users(telegram_chat_id)")
			}

			// Ensure unique index exists on email (don't care about the name)
			var indexExists int64
			s.db.Raw(`
				SELECT COUNT(*) FROM pg_indexes
				WHERE tablename = 'users' AND indexdef LIKE '%email%' AND indexdef LIKE '%UNIQUE%'
			`).Scan(&indexExists)

			if indexExists == 0 {
				s.db.Exec("CREATE UNIQUE INDEX idx_users_email ON users(email)")
			}

			return nil
		}
	}
	return s.db.AutoMigrate(&User{})
}

// Create creates user
func (s *UserStore) Create(user *User) error {
	return s.db.Create(user).Error
}

// GetByEmail gets user by email
func (s *UserStore) GetByEmail(email string) (*User, error) {
	var user User
	err := s.db.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetByID gets user by ID
func (s *UserStore) GetByID(userID string) (*User, error) {
	var user User
	err := s.db.Where("id = ?", userID).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// Count returns the total number of users
func (s *UserStore) Count() (int, error) {
	var count int64
	err := s.db.Model(&User{}).Count(&count).Error
	return int(count), err
}

// GetAllIDs gets all user IDs
func (s *UserStore) GetAllIDs() ([]string, error) {
	var userIDs []string
	err := s.db.Model(&User{}).Order("id").Pluck("id", &userIDs).Error
	return userIDs, err
}

// UpdatePassword updates password
func (s *UserStore) UpdatePassword(userID, passwordHash string) error {
	return s.db.Model(&User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"password_hash": passwordHash,
		"updated_at":    time.Now().UTC(),
	}).Error
}

// GetByTelegramChatID 根据 Telegram Chat ID 获取用户
func (s *UserStore) GetByTelegramChatID(chatID int64) (*User, error) {
	var user User
	err := s.db.Where("telegram_chat_id = ?", chatID).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateTelegramChatID 更新用户的 Telegram Chat ID
func (s *UserStore) UpdateTelegramChatID(userID string, chatID int64) error {
	return s.db.Model(&User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"telegram_chat_id": chatID,
		"updated_at":       time.Now().UTC(),
	}).Error
}

// EnsureAdmin ensures admin user exists
func (s *UserStore) EnsureAdmin() error {
	var count int64
	s.db.Model(&User{}).Where("id = ?", "admin").Count(&count)
	if count > 0 {
		return nil
	}
	return s.Create(&User{
		ID:             "admin",
		Email:          "admin@localhost",
		PasswordHash:   "",
		OTPSecret:      "",
		OTPVerified:    true,
		TelegramChatID: 0,
	})
}
