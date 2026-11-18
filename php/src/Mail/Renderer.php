<?php

declare(strict_types=1);

namespace StudioSchedule\Mail;

use InvalidArgumentException;
use Twig\Environment;
use Twig\Loader\FilesystemLoader;
use Twig\Runtime\EscaperRuntime;
use Twig\TwigFilter;

final readonly class Renderer
{
    public const string DEFAULT_LOCALE = 'en';

    private Environment $twig;

    public function __construct()
    {
        $this->twig = new Environment(new FilesystemLoader(dirname(__DIR__, 2) . '/templates/mail'), [
            'strict_variables' => true,
            'autoescape' => static fn(string $name): string|false => str_ends_with($name, '.html.twig') ? 'mail_html' : false,
        ]);
        $this->twig->getRuntime(EscaperRuntime::class)->setEscaper('mail_html', Formatting::escapeHtml(...));
        $this->twig->addFilter(new TwigFilter('mail_date', Formatting::date(...)));
        $this->twig->addFilter(new TwigFilter('mail_clock', Formatting::clock(...)));
        $this->twig->addFilter(new TwigFilter('mail_url', Formatting::url(...)));
    }

    public function resolveLocale(string $locale): string
    {
        $normalized = strtolower(str_replace('_', '-', Formatting::trimWhitespace($locale)));
        $language = explode('-', $normalized, 2)[0];

        return match ($language) {
            'de', 'en' => $language,
            default => self::DEFAULT_LOCALE,
        };
    }

    /**
     * The keys follow the Go mail template data. Pass date and time values in
     * the school's time zone as DateTimeInterface objects.
     *
     * @param array<string, mixed> $data
     */
    public function render(string $locale, string $template, array $data): Rendered
    {
        $data = TemplateData::prepare($template, $data);
        $data['locale'] = $this->resolveLocale($locale);
        $base = $data['locale'] . '/' . $template;

        $subject = Formatting::singleLine($this->twig->render("$base.subject.txt.twig", $data));
        if ($subject === '') {
            throw new InvalidArgumentException('The mail subject is empty.');
        }

        return new Rendered(
            $subject,
            $this->twig->render("$base.txt.twig", $data),
            $this->twig->render("$base.html.twig", $data),
        );
    }
}
