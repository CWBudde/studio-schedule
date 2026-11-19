-- +goose Up

-- PHP aggregates each finite label combination and duration bucket across workers.
CREATE TABLE php_metrics (
    family VARCHAR(8) NOT NULL,
    name VARCHAR(255) NOT NULL,
    label VARCHAR(8) NOT NULL,
    status INTEGER NOT NULL,
    bucket INTEGER NOT NULL,
    observations BIGINT NOT NULL,
    duration_us BIGINT NOT NULL,
    last_success_us BIGINT NOT NULL,
    PRIMARY KEY (family, name, label, status, bucket)
);

-- +goose Down

DROP TABLE php_metrics;
