<?php

namespace MarshmallowMia\EchoVrMatch\Services;

/** Display names and labels for what match.json holds. */
class Format
{
    public const BLUE = '#3b82f6';

    public const ORANGE = '#f97316';

    /** A social lobby's players (team 3). */
    public const SOCIAL = '#a3e635';

    /** @var array<string, string> */
    private const GAME_TYPES = [
        'echo_arena' => 'Arena',
        'echo_arena_private' => 'Arena (private)',
        'echo_combat' => 'Combat',
        'echo_combat_private' => 'Combat (private)',
        'social_2.0' => 'Lobby',
        'social_2.0_private' => 'Lobby (private)',
        'social_2.0_npe' => 'Lobby (tutorial)',
    ];

    /** @var array<string, string> */
    private const LEVELS = [
        'mpl_arena_a' => 'Arena',
        'mpl_lobby_b2' => 'Lobby',
        'mpl_combat_dyson' => 'Dyson',
        'mpl_combat_combustion' => 'Combustion',
        'mpl_combat_fission' => 'Fission',
        'mpl_combat_gauss' => 'Surge',
    ];

    /** @var array<string, string> */
    private const STATUS = [
        'pre_match' => 'Waiting for players',
        'round_start' => 'Round starting',
        'playing' => 'Playing',
        'score' => 'Goal!',
        'round_over' => 'Round over',
        'post_match' => 'Match over',
        'pre_sudden_death' => 'Sudden death soon',
        'sudden_death' => 'Sudden death',
        'post_sudden_death' => 'Sudden death over',
    ];

    /** @var array<string, string> Match.phase from the game's log. */
    private const PHASES = [
        'pre_match' => 'Waiting for players',
        'round_start' => 'Round starting',
        'playing' => 'Playing',
        'point_scored' => 'Point scored',
        'round_over' => 'Round over',
        'post_match' => 'Match over',
        'returning' => 'Returning to the lobby',
    ];

    /**
     * Levels with a top-down drawing for the live map: the drawing's box in the
     * game's meters as [min z, min x, width (z), height (x)]; z runs left to
     * right, x top to bottom, like /session's positions.
     */
    private const MAPS = [
        'mpl_arena_a' => [-80, -18, 160, 36],
        'mpl_lobby_b2' => [-85, -65, 110, 150], // with its arena and combat sections
    ];

    /** The drawing's file of a level, or null. */
    public static function mapFile(?string $level): ?string
    {
        $level = strtolower((string) $level);
        if (!isset(self::MAPS[$level])) {
            return null;
        }
        $path = dirname(__DIR__, 2) . '/resources/maps/' . $level . '.svg';

        return is_file($path) ? $path : null;
    }

    /**
     * The live map's drawing of a level: its URL (with the file's hash, so a
     * browser fetches it once) and box, or null when there is none.
     *
     * @return ?array{url: string, view: array{0: float, 1: float, 2: float, 3: float}}
     */
    public static function mapDrawing(?string $level): ?array
    {
        $path = self::mapFile($level);
        if ($path === null || !\Illuminate\Support\Facades\Route::has('echovr-match.map')) {
            return null;
        }
        $level = strtolower((string) $level);

        return [
            'url' => route('echovr-match.map', ['map' => $level, 'v' => substr(md5_file($path), 0, 12)]),
            'view' => self::MAPS[$level],
        ];
    }

    public static function isLobby(?array $m): bool
    {
        return str_starts_with(strtolower((string) ($m['game_type'] ?? '')), 'social');
    }

    /** What the match is doing: the API's status, else the log's phase. */
    public static function phase(array $m): string
    {
        if (!empty($m['status'])) {
            return self::status($m['status']);
        }
        $p = $m['phase'] ?? '';

        return $p === '' ? '' : (self::PHASES[$p] ?? str_replace('_', ' ', $p));
    }

    /**
     * The clock: the game's, else (combat) the time since the current round's
     * play began, while it is played.
     */
    public static function clock(array $m): string
    {
        if (!empty($m['clock_display'])) {
            return $m['clock_display'];
        }
        if (!($m['active'] ?? false) || ($m['phase'] ?? '') !== 'playing' || empty($m['round_started_at'])) {
            return '';
        }
        try {
            $s = max(0, \Illuminate\Support\Carbon::parse($m['round_started_at'])->diffInSeconds(now(), true));
        } catch (\Throwable) {
            return '';
        }

        return sprintf('%02d:%02d', intdiv((int) $s, 60), (int) $s % 60);
    }

    /** A ping in ms as a Filament-ish color: fine, high, bad. */
    public static function pingColor(?int $ping, ?float $loss = 0): string
    {
        $ping ??= 0;

        return match (true) {
            $ping >= 150 || ($loss ?? 0) >= 0.05 => '#ef4444',
            $ping >= 90 || ($loss ?? 0) >= 0.01 => '#eab308',
            default => 'inherit',
        };
    }

    /** A small inline SVG line of pings, oldest left. */
    public static function sparkline(array $values, string $color = 'currentColor'): string
    {
        $values = array_values(array_map('intval', $values));
        $n = count($values);
        if ($n < 2) {
            return '';
        }
        $w = 60;
        $h = 16;
        $max = max(100, max($values));
        $pts = [];
        foreach ($values as $i => $v) {
            $pts[] = round($i * $w / ($n - 1), 1) . ',' . round($h - 1 - ($v / $max) * ($h - 2), 1);
        }

        return sprintf(
            '<svg width="%d" height="%d" viewBox="0 0 %d %d" style="vertical-align:middle;overflow:visible" aria-hidden="true"><polyline fill="none" stroke="%s" stroke-width="1.25" points="%s"/></svg>',
            $w, $h, $w, $h, e($color === 'inherit' ? 'currentColor' : $color), implode(' ', $pts)
        );
    }

    public static function mode(?string $gameType): string
    {
        return $gameType ? (self::GAME_TYPES[strtolower($gameType)] ?? $gameType) : '—';
    }

    public static function level(?string $level): string
    {
        return $level ? (self::LEVELS[strtolower($level)] ?? $level) : '—';
    }

    /** "Arena" or "Combat · Dyson". */
    public static function title(array $m): string
    {
        $mode = self::mode($m['game_type'] ?? null);
        $level = self::level($m['map'] ?? null);

        return ($level === '—' || strcasecmp($level, $mode) === 0) ? $mode : "$mode · $level";
    }

    public static function status(?string $status): string
    {
        return $status ? (self::STATUS[$status] ?? str_replace('_', ' ', $status)) : '';
    }

    /** The supervisor's state as a label and a Filament color. */
    public static function state(?string $state): array
    {
        return match ($state) {
            'in_session' => ['In a match', 'success'],
            'registered' => ['Waiting for a match', 'info'],
            'starting' => ['Starting', 'warning'],
            'awaiting_approval' => ['Waiting for sign-in approval', 'warning'],
            'stopping' => ['Ending match', 'warning'],
            'backoff' => ['Restarting after an error', 'danger'],
            'failed' => ['Failed', 'danger'],
            'stopped' => ['Stopped', 'gray'],
            default => ['Unknown', 'gray'],
        };
    }

    public static function duration(int $seconds): string
    {
        $m = intdiv(max($seconds, 0), 60);

        return $m >= 60 ? sprintf('%dh %02dm', intdiv($m, 60), $m % 60) : sprintf('%dm %02ds', $m, $seconds % 60);
    }

    /** A score: goals as a number, combat's capture progress in percent. */
    public static function points(mixed $value, bool $percent = false): string
    {
        $v = (float) ($value ?? 0);

        return $percent ? round($v) . '%' : (string) (fmod($v, 1.0) === 0.0 ? (int) $v : round($v, 1));
    }

    public static function teamColor(?string $team): string
    {
        return match ($team) {
            'blue' => self::BLUE,
            'orange' => self::ORANGE,
            'social' => self::SOCIAL,
            default => 'inherit',
        };
    }

    public static function time(?string $iso): string
    {
        if (!$iso) {
            return '';
        }
        try {
            return \Illuminate\Support\Carbon::parse($iso)->timezone(config('app.timezone'))->isoFormat('LT');
        } catch (\Throwable) {
            return '';
        }
    }

    public static function when(?string $iso): string
    {
        if (!$iso) {
            return '';
        }
        try {
            return \Illuminate\Support\Carbon::parse($iso)->diffForHumans();
        } catch (\Throwable) {
            return '';
        }
    }
}
