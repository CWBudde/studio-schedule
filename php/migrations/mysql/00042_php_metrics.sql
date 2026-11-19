-- +goose Up

-- PHP aggregates each finite label combination and duration bucket across workers.
CREATE TABLE php_metrics (
    family VARCHAR(8) NOT NULL,
    name VARCHAR(255) NOT NULL,
    label VARCHAR(8) NOT NULL,
    status INT NOT NULL,
    bucket INT NOT NULL,
    observations BIGINT NOT NULL,
    duration_us BIGINT NOT NULL,
    last_success_us BIGINT NOT NULL,
    PRIMARY KEY (family, name, label, status, bucket)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down

DROP TABLE php_metrics;
