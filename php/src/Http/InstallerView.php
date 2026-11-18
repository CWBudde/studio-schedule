<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;

final class InstallerView
{
    public static function page(ResponseInterface $response, string $content, int $status = 200): ResponseInterface
    {
        $response->getBody()->write(<<<HTML
            <!doctype html>
            <html lang="en">
            <head>
                <meta charset="utf-8">
                <meta name="viewport" content="width=device-width, initial-scale=1">
                <title>Studio Schedule setup</title>
                <style>
                    body { font: 1rem/1.5 system-ui, sans-serif; color: #182035; background: #f5f6fa; margin: 0; }
                    main { max-width: 38rem; margin: 3rem auto; padding: 1.5rem; background: #fff; }
                    h1 { margin: 0 0 1rem; font-size: 1.65rem; }
                    label { display: block; margin-top: .75rem; }
                    input, select, button { font: inherit; }
                    input:not([type=checkbox]), select { display: block; margin-top: .2rem; padding: .4rem; width: min(92%, 28rem); }
                    button { margin-top: 1rem; padding: .45rem .8rem; }
                    @media (max-width: 42rem) { main { margin: 0; } }
                </style>
            </head>
            <body><main><h1>Studio Schedule setup</h1>$content</main></body>
            </html>
            HTML);

        return $response->withStatus($status)
            ->withHeader('Content-Type', 'text/html; charset=utf-8')
            ->withHeader('Cache-Control', 'no-store');
    }

    public static function message(string $message): string
    {
        return '<p>' . self::escape($message) . '</p>';
    }

    public static function install(string $path, #[\SensitiveParameter] string $token): string
    {
        $form = self::form($path, $token, 'install');

        return $form . <<<'HTML'
            <label>School name<input name="schoolName" required maxlength="200" autocomplete="organization"></label>
            <label>School time zone<input name="timezone" required placeholder="Europe/Berlin"></label>
            <label>Default language<select name="locale"><option value="en">English</option><option value="de">Deutsch</option></select></label>
            <label>Currency<input name="currency" value="EUR" required maxlength="3"></label>
            <label>Administrator given name<input name="givenName" required autocomplete="given-name"></label>
            <label>Administrator family name<input name="familyName" required autocomplete="family-name"></label>
            <label>Administrator email<input type="email" name="email" maxlength="254" required autocomplete="email"></label>
            <button type="submit">Set up school</button></form>
            HTML;
    }

    /** @param non-empty-list<string> $pending */
    public static function upgrade(
        string $path,
        #[\SensitiveParameter]
        string $token,
        array $pending,
        bool $backupRequired,
    ): string {
        $list = '<ul>' . implode('', array_map(static fn(string $file): string => '<li>' . self::escape($file) . '</li>', $pending)) . '</ul>';
        $form = self::form($path, $token, 'upgrade');
        $backup = $backupRequired
            ? '<label><input type="checkbox" name="backupVerified" value="yes" required> I stopped the site and scheduled writes and verified a full pre-upgrade backup.</label>'
            : '';

        return '<h2>Pending migrations</h2>' . $list . $form . <<<'HTML'
            <label>Administrator email<input type="email" name="email" maxlength="254" required autocomplete="username"></label>
            <label>Administrator password<input type="password" name="password" maxlength="512" required autocomplete="current-password"></label>
            HTML
            . $backup . '<button type="submit">Apply migrations</button></form>';
    }

    public static function copy(string $path, #[\SensitiveParameter] string $token): string
    {
        return self::message('The school is set up and all migrations are applied.')
            . '<h2>Copy the database</h2>'
            . self::message('Stop site and scheduled writes before copying. The target must be empty and migrated to this release. Copy between PostgreSQL and either MySQL or MariaDB.')
            . self::form($path, $token, 'copy')
            . <<<'HTML'
                <label>Administrator email<input type="email" name="email" maxlength="254" required autocomplete="username"></label>
                <label>Administrator password<input type="password" name="password" maxlength="512" required autocomplete="current-password"></label>
                <label>Target database URL<input type="password" name="targetDatabaseUrl" required autocomplete="off" spellcheck="false"></label>
                <button type="submit">Copy database</button></form>
                HTML;
    }

    public static function setupLink(#[\SensitiveParameter] string $url, string $expires): string
    {
        return '<p>The school and its first administrator are set up.</p><p><a href="' . self::escape($url)
            . '">Set the administrator password</a>. The link works once and expires at ' . self::escape($expires) . '.</p>';
    }

    private static function form(string $path, #[\SensitiveParameter] string $token, string $action): string
    {
        return '<form method="post" action="' . self::escape($path) . '"><input type="hidden" name="token" value="'
            . self::escape($token) . '"><input type="hidden" name="action" value="' . self::escape($action) . '">';
    }

    private static function escape(string $text): string
    {
        return htmlspecialchars($text, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
    }
}
