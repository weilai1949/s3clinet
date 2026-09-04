package store

import "github.com/weilai1949/s3clinet/server/internal/model"

// AccountStore 账号持久化抽象（JSON 文件或 SQLite）。
type AccountStore interface {
	List() ([]*model.Account, error)
	Get(id string) (*model.Account, error)
	Create(a *model.Account) (*model.Account, error)
	Update(id string, a *model.Account) (*model.Account, error)
	Delete(id string) error
	Ping() error
	Close() error
}
