-- ALTER TYPE ... ADD VALUE cannot run inside a multi-statement transaction
-- block on Postgres (it errors with "ALTER TYPE ... ADD VALUE cannot run
-- inside a transaction block" pre-12, and "unsafe use of new value" if used
-- in the same transaction on 12+). golang-migrate sends each file as one
-- batch, so this file must contain only this single statement to avoid being
-- implicitly wrapped alongside other DDL.
ALTER TYPE channel_type ADD VALUE 'email';
