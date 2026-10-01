-- Administrators are managed in the database. The superadmin is deliberately
-- NOT stored here: it comes from the SUPERADMIN_EMAIL environment variable so
-- that a single account always sits above the database-managed admins.

CREATE TABLE IF NOT EXISTS admin_emails (
    email VARCHAR(255) PRIMARY KEY
);
