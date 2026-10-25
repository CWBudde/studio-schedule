import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Download, FileUp, FlaskConical, Upload } from 'lucide-react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type ImportField,
  type ImportInspection,
  type ImportMapping,
  type ImportResult,
  inspectImportMutation,
  runImportMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { Label } from '@/components/ui/label';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { SelectField } from '@/features/admin/crud/fields';
import { ImportHistory } from './ImportHistory';
import {
  assign,
  base64Of,
  guardianFields,
  initialMapping,
  mappingComplete,
  maxImportBytes,
  memberFields,
  rememberedMapping,
  rememberMapping,
  toggleNote,
} from './importMapping';
import { ImportResults } from './ImportResults';
import { useFieldLabel } from './useFieldLabel';
import sampleCsv from './members-sample.csv?raw';

/** "Not imported" in the selects, which have no empty value. */
const none = '\u0000none';

/** The uploaded file, as the API takes it. */
type ChosenFile = { fileName: string; content: string };

/**
 * The member import (/admin/import), for the office (ADR 0015): a CSV file
 * is read, its columns mapped to the fields (matched by their names and
 * remembered in this browser), tried in a dry run that writes nothing, and
 * then imported. Below, the runs so far.
 */
export function ImportPage() {
  const { t } = useTranslation('imports');
  const queryClient = useQueryClient();
  const inspect = useMutation(inspectImportMutation());
  const run = useMutation(runImportMutation(queryClient));
  const [file, setFile] = useState<ChosenFile | null>(null);
  const [inspection, setInspection] = useState<ImportInspection | null>(null);
  const [mapping, setMapping] = useState<ImportMapping>({ columns: {} });
  const [result, setResult] = useState<ImportResult | null>(null);
  const [tooLarge, setTooLarge] = useState(false);

  const choose = async (chosen: File | undefined) => {
    setResult(null);
    setInspection(null);
    setFile(null);
    run.reset();
    setTooLarge(false);
    if (!chosen) return;
    if (chosen.size > maxImportBytes) {
      setTooLarge(true);
      return;
    }
    const body = { fileName: chosen.name, content: await base64Of(chosen) };
    inspect.mutate(body, {
      onSuccess: (insp) => {
        setFile(body);
        setInspection(insp);
        setMapping(initialMapping(insp, rememberedMapping()));
      },
    });
  };

  const changeMapping = (next: ImportMapping) => {
    setMapping(next);
    rememberMapping(next);
    // A result of another mapping no longer applies.
    setResult(null);
    run.reset();
  };

  const submit = (apply: boolean) => {
    if (!file) return;
    run.mutate(
      { ...file, mapping, apply },
      {
        onSuccess: (res) => {
          setResult(res);
          if (res.applied) {
            toast.success(
              t('applied', {
                created: res.counts.created,
                updated: res.counts.updated,
                failed: res.counts.failed,
              }),
            );
          }
        },
      },
    );
  };

  const downloadSample = () => {
    const url = URL.createObjectURL(new Blob([sampleCsv], { type: 'text/csv;charset=utf-8' }));
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = 'members-sample.csv';
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 60_000);
  };

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('title')}</h1>
        <p className="max-w-prose text-muted-foreground">{t('description')}</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t('file.title')}</CardTitle>
          <CardDescription>{t('file.description')}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <FilePicker busy={inspect.isPending} onChoose={(f) => void choose(f)} />
          {tooLarge && (
            <p role="alert" className="text-sm text-destructive">
              {t('file.tooLarge')}
            </p>
          )}
          <ProblemAlert error={inspect.error} messages={{ 422: t('file.unreadable') }} />
          {inspection && file && (
            <p className="text-sm text-muted-foreground">
              {t('file.summary', {
                name: file.fileName,
                count: inspection.rows,
                delimiter: t(`file.delimiters.${inspection.delimiter}`),
                encoding: inspection.encoding === 'utf-8' ? 'UTF-8' : 'Windows-1252',
              })}
            </p>
          )}
          <div className="flex flex-wrap items-center gap-2 border-t pt-4">
            <Button variant="outline" size="sm" onClick={downloadSample}>
              <Download aria-hidden="true" />
              {t('file.sample')}
            </Button>
            <span className="text-sm text-muted-foreground">{t('file.sampleHint')}</span>
          </div>
        </CardContent>
      </Card>

      {inspection && (
        <Card>
          <CardHeader>
            <CardTitle>{t('mapping.title')}</CardTitle>
            <CardDescription>{t('mapping.description')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-6">
            <MappingForm inspection={inspection} mapping={mapping} onChange={changeMapping} />
            <Preview inspection={inspection} />
            <div className="space-y-3 border-t pt-4">
              <ProblemAlert error={run.error} messages={{ 422: t('mapping.mismatch') }} />
              {!mappingComplete(mapping) && (
                <p className="text-sm text-muted-foreground">{t('mapping.required')}</p>
              )}
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  disabled={!mappingComplete(mapping) || run.isPending}
                  onClick={() => submit(false)}
                >
                  <FlaskConical aria-hidden="true" />
                  {run.isPending && !run.variables.apply ? t('trying') : t('try')}
                </Button>
                {result && !result.applied && (
                  <Button disabled={run.isPending} onClick={() => submit(true)}>
                    <Upload aria-hidden="true" />
                    {run.isPending && run.variables.apply
                      ? t('importing')
                      : t('import', { count: result.counts.rows - result.counts.failed })}
                  </Button>
                )}
              </div>
              <p className="text-xs text-muted-foreground">{t('tryHint')}</p>
            </div>
          </CardContent>
        </Card>
      )}

      {result && <ImportResults result={result} />}

      <ImportHistory />
    </div>
  );
}

function FilePicker({
  busy,
  onChoose,
}: {
  busy: boolean;
  onChoose: (f: File | undefined) => void;
}) {
  const { t } = useTranslation('imports');
  const id = useId();
  return (
    <div className="grid max-w-md gap-1.5">
      <Label htmlFor={id}>{t('file.label')}</Label>
      <div className="flex items-center gap-2">
        <FileUp className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <input
          id={id}
          type="file"
          accept=".csv,.txt,text/csv,text/plain"
          disabled={busy}
          className="block w-full text-sm file:mr-3 file:rounded-md file:border file:border-input file:bg-background file:px-3 file:py-1.5 file:text-sm file:font-medium hover:file:bg-muted"
          onChange={(e) => onChoose(e.target.files?.[0])}
        />
      </div>
      <p className="text-xs text-muted-foreground">{busy ? t('file.reading') : t('file.hint')}</p>
    </div>
  );
}

function MappingForm({
  inspection,
  mapping,
  onChange,
}: {
  inspection: ImportInspection;
  mapping: ImportMapping;
  onChange: (m: ImportMapping) => void;
}) {
  const { t } = useTranslation('imports');
  const label = useFieldLabel();
  const options = [
    { value: none, label: t('mapping.none') },
    ...inspection.headers.map((h) => ({ value: h, label: h })),
  ];
  const select = (field: ImportField) => (
    <SelectField
      key={field}
      label={label(field)}
      placeholder={t('mapping.none')}
      options={options}
      value={mapping.columns[field] ?? none}
      onChange={(v) => onChange(assign(mapping, field, v === none ? undefined : v))}
    />
  );
  const mapped = new Set(Object.values(mapping.columns));
  const free = inspection.headers.filter((h) => !mapped.has(h));

  return (
    <div className="space-y-6">
      <fieldset className="space-y-3">
        <legend className="mb-2 font-medium">{t('mapping.member')}</legend>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{memberFields.map(select)}</div>
      </fieldset>
      {([1, 2] as const).map((slot) => (
        <fieldset key={slot} className="space-y-3">
          <legend className="mb-2 font-medium">{t('mapping.guardian', { slot })}</legend>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {guardianFields(slot).map(select)}
          </div>
        </fieldset>
      ))}
      <fieldset className="space-y-2">
        <legend className="mb-1 font-medium">{t('mapping.notesTitle')}</legend>
        <p className="text-sm text-muted-foreground">{t('mapping.notesDescription')}</p>
        {free.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('mapping.noFreeColumns')}</p>
        ) : (
          <div className="flex flex-wrap gap-x-6 gap-y-2">
            {free.map((h) => (
              <NoteToggle
                key={h}
                header={h}
                checked={(mapping.notes ?? []).includes(h)}
                onChange={(on) => onChange(toggleNote(mapping, h, on))}
              />
            ))}
          </div>
        )}
      </fieldset>
    </div>
  );
}

function NoteToggle({
  header,
  checked,
  onChange,
}: {
  header: string;
  checked: boolean;
  onChange: (on: boolean) => void;
}) {
  const id = useId();
  return (
    <div className="flex items-center gap-2">
      <Checkbox id={id} checked={checked} onCheckedChange={(v) => onChange(v === true)} />
      <Label htmlFor={id} className="font-normal">
        {header}
      </Label>
    </div>
  );
}

/** The file's first rows, as read. */
function Preview({ inspection }: { inspection: ImportInspection }) {
  const { t } = useTranslation('imports');
  if (inspection.sample.length === 0) return null;
  return (
    <div className="space-y-2">
      <h3 className="font-medium">{t('mapping.preview')}</h3>
      <div className="overflow-x-auto rounded-md border">
        <Table aria-label={t('mapping.preview')}>
          <TableHeader>
            <TableRow>
              {inspection.headers.map((h) => (
                <TableHead key={h} className="whitespace-nowrap">
                  {h}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {/* The rows have no key of their own; their order is fixed. */}
            {inspection.sample.map((row, i) => (
              <TableRow key={i}>
                {(row ?? []).map((cell, j) => (
                  <TableCell key={j} className="whitespace-nowrap">
                    {cell}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
