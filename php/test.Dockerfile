FROM php:8.3-cli
COPY --from=composer:2 /usr/bin/composer /usr/local/bin/composer
RUN apt-get update \
    && apt-get install -y --no-install-recommends libjpeg62-turbo-dev libpng-dev libpq-dev poppler-utils unzip \
    && docker-php-ext-configure gd --with-jpeg \
    && docker-php-ext-install -j2 gd pdo_pgsql pdo_mysql \
    && pecl install pcov-1.0.12 \
    && docker-php-ext-enable pcov \
    && rm -rf /var/lib/apt/lists/*
