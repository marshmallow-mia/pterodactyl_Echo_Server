<?php

namespace MarshmallowMia\EchoVrMatch\Services;

/** Display names and labels for what match.json holds. */
class Format
{
    public const BLUE = '#3b82f6';

    public const ORANGE = '#f97316';

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
