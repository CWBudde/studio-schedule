<?php

declare(strict_types=1);

$report = $argv[1] ?? '';
$document = new DOMDocument();
if ($report === '' || !$document->load($report, LIBXML_NONET)) {
    fwrite(STDERR, "usage: php tools/check-coverage.php report.xml\n");
    exit(2);
}

$xpath = new DOMXPath($document);
$metrics = $xpath->query('/coverage/project//file/metrics');
$namespaces = [];
$covered = 0;
$total = 0;
foreach ($metrics ?: [] as $metric) {
    if (!$metric instanceof DOMElement || !$metric->parentNode instanceof DOMElement) {
        continue;
    }

    $namespace = $xpath->evaluate('string((class/@namespace)[1])', $metric->parentNode);
    if (!is_string($namespace) || $namespace === '') {
        $namespace = '(global)';
    }
    $statements = (int) $metric->getAttribute('statements');
    $hits = (int) $metric->getAttribute('coveredstatements');
    $namespaces[$namespace]['statements'] = ($namespaces[$namespace]['statements'] ?? 0) + $statements;
    $namespaces[$namespace]['covered'] = ($namespaces[$namespace]['covered'] ?? 0) + $hits;
    $total += $statements;
    $covered += $hits;
}

if ($total === 0) {
    fwrite(STDERR, "PHP coverage report contains no statements\n");
    exit(2);
}

printf("PHP coverage %.2f%% (%d of %d statements)\n", 100 * $covered / $total, $covered, $total);
$pass = 100 * $covered >= 95 * $total;
ksort($namespaces);
foreach ($namespaces as $namespace => $count) {
    if ($count['statements'] === 0) {
        continue;
    }

    $percentage = 100 * $count['covered'] / $count['statements'];
    printf("  %-34s %6.2f%% (%d of %d)\n", $namespace, $percentage, $count['covered'], $count['statements']);
    $pass = $pass && 100 * $count['covered'] >= 80 * $count['statements'];
}
if (!$pass) {
    fwrite(STDERR, "PHP coverage needs 95% overall and 80% per namespace\n");
    exit(1);
}
