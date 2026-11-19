<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use InvalidArgumentException;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Psr\Http\Message\ResponseInterface;
use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\Psr7\Factory\StreamFactory;
use Slim\Psr7\Response;
use StudioSchedule\Http\MemberImportEndpoint;
use StudioSchedule\Service\MemberCsv;
use StudioSchedule\Service\MemberImportHeaders;

final class MemberCsvTest extends TestCase
{
    public function testQuotedUtf8HeaderAndMultilineCellsRetainPhysicalLineNumbers(): void
    {
        $csv = MemberCsv::parse("\xEF\xBB\xBF\"Member; No\",First Name,Last Name,Notes\r\n"
            . "17, Ada ,Muster,\"one\r\ntwo \"\"quoted\"\"\"\r\n\r\n"
            . "18,Bea,Sample,,\r\n19,Carla");
        self::assertSame('utf-8', $csv->encoding);
        self::assertSame(',', $csv->delimiter);
        self::assertSame(['Member; No', 'First Name', 'Last Name', 'Notes'], $csv->headers);
        self::assertSame([
            ['line' => 2, 'cells' => ['17', 'Ada', 'Muster', "one\ntwo \"quoted\""]],
            ['line' => 5, 'cells' => ['18', 'Bea', 'Sample', '']],
            ['line' => 6, 'cells' => ['19', 'Carla', '', '']],
        ], $csv->rows);
    }

    public function testWindows1252TabFileAndMappingSuggestions(): void
    {
        $csv = MemberCsv::parse("Mitgliedsnummer\tVorname\tNachname\tE-Mail Elternteil 1\tTelefon Vater\t"
            . "Parent 2 given name\tName Mutter\tExtra\tMember ID\r\nA1\tK\xE4the\tS\xFC\xDF\t\t\t\t\t\x80\x81\tA1");
        self::assertSame('windows-1252', $csv->encoding);
        self::assertSame("\t", $csv->delimiter);
        self::assertSame('Käthe', $csv->rows[0]['cells'][1] ?? null);
        self::assertSame('Süß', $csv->rows[0]['cells'][2] ?? null);
        self::assertSame('€�', $csv->rows[0]['cells'][7] ?? null);
        self::assertSame([
            'memberNo' => 'Mitgliedsnummer', 'givenName' => 'Vorname', 'familyName' => 'Nachname',
            'guardian1Email' => 'E-Mail Elternteil 1', 'guardian2Phone' => 'Telefon Vater',
            'guardian2GivenName' => 'Parent 2 given name', 'guardian1FamilyName' => 'Name Mutter',
        ], MemberImportHeaders::suggest($csv->headers));
    }

    public function testSpreadsheetQuotesAndEmptyRows(): void
    {
        $csv = MemberCsv::parse("No;Note, second;Value\n  ; ;  \n"
            . "0;ordinary\"quote;one\n1;\"a\"b\";2\n3;\"unclosed\nlast");
        self::assertSame(';', $csv->delimiter);
        self::assertSame([
            ['line' => 3, 'cells' => ['0', 'ordinary"quote', 'one']],
            ['line' => 4, 'cells' => ['1', 'a"b', '2']],
            ['line' => 5, 'cells' => ['3', "unclosed\nlast", '']],
        ], $csv->rows);
    }

    public function testInspectionAcceptsTheFullDecodedFileLimitAndOmitsEmptyMappingKeys(): void
    {
        $file = "No,Other\n1,";
        $file .= str_repeat(' ', MemberCsv::MAX_BYTES - strlen($file));
        $response = self::inspect(['fileName' => 'fixture.csv', 'content' => base64_encode($file)]);
        self::assertSame(200, $response->getStatusCode());
        self::assertSame('{"encoding":"utf-8","delimiter":"comma","headers":["No","Other"],"rows":1,"sample":[["1",""]],"suggested":{"columns":{}}}', (string) $response->getBody());

        $overflow = self::inspect(['fileName' => 'fixture.csv', 'content' => base64_encode($file . ' ')]);
        self::assertSame(422, $overflow->getStatusCode());
        self::assertStringContainsString('"location":"body.content"', (string) $overflow->getBody());
        self::assertStringContainsString('larger than 1024 KiB', (string) $overflow->getBody());

        $headerOnly = self::inspect(['fileName' => 'fixture.csv', 'content' => base64_encode("Vorname\tNachname\n")]);
        self::assertSame(200, $headerOnly->getStatusCode());
        self::assertStringContainsString('"sample":[]', (string) $headerOnly->getBody());
        self::assertStringContainsString('"delimiter":"tab"', (string) $headerOnly->getBody());
    }

    public function testInspectionRefusesInvalidUploadFields(): void
    {
        $name = self::inspect(['fileName' => '', 'content' => 'TmFtZQo=']);
        self::assertSame(422, $name->getStatusCode());
        self::assertStringContainsString('"location":"body.fileName"', (string) $name->getBody());
        $invalid = self::inspect(['fileName' => 'fixture.csv', 'content' => 'Tm FtZQo=']);
        self::assertSame(422, $invalid->getStatusCode());
        self::assertStringContainsString('"location":"body.content"', (string) $invalid->getBody());
        $extra = self::inspect(['fileName' => 'fixture.csv', 'content' => 'TmFtZQo=', 'apply' => true]);
        self::assertSame(422, $extra->getStatusCode());
        self::assertStringContainsString('"location":"body.apply"', (string) $extra->getBody());
        self::assertSame(400, self::inspect(null)->getStatusCode());
    }

    /** @param array<string,string|bool>|null $payload */
    private static function inspect(?array $payload): ResponseInterface
    {
        $request = (new ServerRequestFactory())->createServerRequest('POST', '/api/v1/admin/imports/inspect');
        if ($payload !== null) {
            $request = $request->withHeader('Content-Type', 'application/json')
                ->withBody((new StreamFactory())->createStream(json_encode($payload, JSON_THROW_ON_ERROR)));
        }

        return (new MemberImportEndpoint())($request, new Response());
    }

    #[DataProvider('invalidFiles')]
    public function testInvalidFile(string $content, string $message): void
    {
        $this->expectException(InvalidArgumentException::class);
        $this->expectExceptionMessage($message);
        MemberCsv::parse($content);
    }

    /** @return iterable<string,array{string,string}> */
    public static function invalidFiles(): iterable
    {
        yield 'empty' => [" \r\n \t", 'file is empty'];
        yield 'unnamed column' => ["No;;Name\n1;;Ada\n", 'column 2'];
        yield 'duplicate folded header' => ["Name;name\nAda;Muster\n", 'twice'];
        yield 'overflowing row' => ["No;Name\n\n2;Ada;Unexpected", 'line 3 has more cells'];
        yield 'too large' => [str_repeat('a', 1_048_577), 'larger than 1024 KiB'];
        yield 'too many rows' => ["No\n" . str_repeat("1\n", 5001), 'more than 5000 rows'];
        yield 'too many columns' => [implode(';', range(0, 100)), 'more than 100 columns'];
    }
}
