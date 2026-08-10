package db

// QMS opened its own database connection here. The subscriptions service owns
// the connection now and hands it to the /v1 handlers, so the connection setup
// that used to live in this file is gone.
