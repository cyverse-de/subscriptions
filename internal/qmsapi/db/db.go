package db

// QMS opened its own database connection here. The subscriptions service owns
// the connection now and layers GORM over it with InitGORMConnection, so the
// connection setup that used to live in this file is gone.
