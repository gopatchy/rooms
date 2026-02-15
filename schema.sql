DO $$ BEGIN
    CREATE TYPE constraint_kind AS ENUM ('happy', 'ok', 'never');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE constraint_level AS ENUM ('student', 'parent', 'admin');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS trips (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS trip_admins (
    id BIGSERIAL PRIMARY KEY,
    trip_id BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    UNIQUE(trip_id, email)
);

CREATE TABLE IF NOT EXISTS students (
    id BIGSERIAL PRIMARY KEY,
    trip_id BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    UNIQUE(trip_id, email)
);

CREATE TABLE IF NOT EXISTS parents (
    id BIGSERIAL PRIMARY KEY,
    student_id BIGINT NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    UNIQUE(student_id, email)
);

CREATE TABLE IF NOT EXISTS roommate_constraints (
    id BIGSERIAL PRIMARY KEY,
    student_a_id BIGINT NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    student_b_id BIGINT NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    kind constraint_kind NOT NULL,
    level constraint_level NOT NULL,
    UNIQUE(student_a_id, student_b_id, level)
);
