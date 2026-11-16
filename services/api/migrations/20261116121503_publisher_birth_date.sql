-- +goose Up
ALTER TABLE learners ADD COLUMN birth_date date
    CHECK (birth_date >= DATE '1900-01-01' AND birth_date < 'infinity'::date);

-- +goose Down
ALTER TABLE learners DROP COLUMN birth_date;
