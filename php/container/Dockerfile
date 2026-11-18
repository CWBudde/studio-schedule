FROM php:8.3-fpm

RUN apt-get update \
    && apt-get install -y --no-install-recommends nginx libjpeg62-turbo-dev libpng-dev libpq-dev \
    && docker-php-ext-configure gd --with-jpeg \
    && docker-php-ext-install -j2 gd pdo_pgsql pdo_mysql \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/studio-schedule
COPY --chown=root:www-data app/ app/
COPY --chown=root:www-data public/ public/
COPY --chown=root:www-data nginx.conf php-fpm.conf /etc/studio-schedule/
COPY serve.sh /usr/local/bin/studio-schedule-php
RUN cp "$PHP_INI_DIR/php.ini-production" "$PHP_INI_DIR/php.ini" \
    && chmod 0755 /usr/local/bin/studio-schedule-php \
    && ln -s /opt/studio-schedule/app/bin/studio-schedule /usr/local/bin/studio-schedule

ENV LOG_FORMAT=json HTTP_ADDR=:8080
USER www-data:www-data
EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["studio-schedule", "healthcheck", "--addr", "127.0.0.1:8080"]

ENTRYPOINT ["studio-schedule-php"]
CMD ["serve"]
