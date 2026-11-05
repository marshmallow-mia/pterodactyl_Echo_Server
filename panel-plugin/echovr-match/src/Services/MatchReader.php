<?php

namespace MarshmallowMia\EchoVrMatch\Services;

use App\Enums\ContainerStatus;
use App\Models\Server;
use App\Repositories\Daemon\DaemonFileRepository;
use Illuminate\Contracts\Filesystem\FileNotFoundException;
use Illuminate\Support\Facades\Cache;
use Throwable;

/**
 * Reads what evr-supervisor writes into a server's volume (status.json,
 * match.json, match-history.json, evr-settings.json) through Wings, so the
 * panel needs no access to the node's disk.
 */
class MatchReader
{
    public const MISSING = 'missing';

    public const UNREACHABLE = 'unreachable';

    public static function isEcho(?Server $server): bool
    {
        if (!$server instanceof Server || !$server->egg) {
            return false;
        }

        $tags = $server->egg->tags ?? [];

        return $server->egg->uuid === config('echovr-match.egg_uuid')
            || (is_array($tags) && in_array(config('echovr-match.egg_tag'), $tags, true));
    }

    /**
     * @return array{data: ?array<string, mixed>, error: ?string}
     */
    public static function file(Server $server, string $name, bool $fresh = false): array
    {
        $key = "echovr-match.$server->uuid.$name";
        if ($fresh) {
            Cache::forget($key);
        }

        return Cache::remember($key, now()->addSeconds((int) config('echovr-match.cache_seconds', 2)), function () use ($server, $name) {
            try {
                $body = (string) app(DaemonFileRepository::class)->setServer($server)->getContent($name, 4 * 1024 * 1024);
                $data = json_decode($body, true);

                return ['data' => is_array($data) ? $data : null, 'error' => is_array($data) ? null : 'unreadable'];
            } catch (FileNotFoundException) {
                return ['data' => null, 'error' => self::MISSING];
            } catch (Throwable $e) {
                report($e);

                return ['data' => null, 'error' => self::UNREACHABLE];
            }
        });
    }

    /**
     * The server's live.json (the map's positions, rewritten several times a
     * second). Every viewer shares one read per live_cache_ms; Laravel's cache
     * keeps nothing under a second, so the entry carries its own read time.
     *
     * @return ?array<string, mixed> null when there is none (no match, or live positions off)
     */
    public static function live(Server $server): ?array
    {
        $key = "echovr-match.$server->uuid.live";
        $maxAge = max(0, (int) config('echovr-match.live_cache_ms', 150)) / 1000;
        $hit = Cache::get($key);
        if (is_array($hit) && microtime(true) - ($hit['read_at'] ?? 0) < $maxAge) {
            return $hit['data'];
        }
        try {
            $body = (string) app(DaemonFileRepository::class)->setServer($server)->getContent('live.json', 256 * 1024);
            $data = json_decode($body, true);
            $data = is_array($data) ? $data : null;
        } catch (FileNotFoundException) {
            $data = null;
        } catch (Throwable $e) {
            report($e);
            $data = null;
        }
        Cache::put($key, ['read_at' => microtime(true), 'data' => $data], 2);

        return $data;
    }

    /** @return array{data: ?array<string, mixed>, error: ?string} */
    public static function match(Server $server): array
    {
        $m = self::file($server, 'match.json');
        if (!empty($m['data']['active']) && !self::inSession(self::status($server)['data'] ?? null, $m['data'])) {
            // Left active by a game that is gone (the supervisor fixes it at its next start).
            $m['data']['active'] = false;
            $m['data']['ended_at'] ??= $m['data']['updated_at'] ?? null;
        }

        return $m;
    }

    /** Whether status.json says the server is in this match's session. */
    public static function inSession(?array $status, array $match): bool
    {
        $state = $status['state'] ?? '';
        if (!in_array($state, ['in_session', 'stopping'], true)) {
            return false;
        }
        $sid = strtolower((string) ($status['session'] ?? ''));

        return $sid === '' || $sid === strtolower((string) ($match['session_id'] ?? ''));
    }

    /** @return array{data: ?array<string, mixed>, error: ?string} */
    public static function status(Server $server): array
    {
        return self::file($server, 'status.json');
    }

    /** @return array<int, array<string, mixed>> */
    public static function history(Server $server): array
    {
        $h = self::file($server, 'match-history.json');

        return $h['data']['matches'] ?? [];
    }

    /** Whether the server's session API setting is on (it is unless switched off). */
    public static function apiSetting(Server $server): bool
    {
        $s = self::file($server, 'evr-settings.json');

        return ($s['data']['session_api'] ?? true) !== false;
    }

    /**
     * Switches the session API. A running server gets the console command, so
     * its supervisor applies it (an idle game restarts, one in a match picks it
     * up after the match); a stopped one gets the setting in its volume.
     *
     * @return string the translation key of what happened
     */
    public static function setApi(Server $server, bool $on): string
    {
        return self::setSwitch($server, 'api', 'session_api', $on);
    }

    /** Whether the server streams nEVR's telemetry (the play-by-play) to its supervisor; on unless switched off. */
    public static function telemetrySetting(Server $server): bool
    {
        $s = self::file($server, 'evr-settings.json');

        return ($s['data']['telemetry'] ?? true) !== false;
    }

    /** Switches the telemetry stream like setApi. */
    public static function setTelemetry(Server $server, bool $on): string
    {
        return self::setSwitch($server, 'telemetry', 'telemetry', $on);
    }

    private static function setSwitch(Server $server, string $command, string $key, bool $on): string
    {
        $state = $server->retrieveStatus();
        if (in_array($state, [ContainerStatus::Running, ContainerStatus::Starting], true)) {
            $server->send($command . ' ' . ($on ? 'on' : 'off'));
            $result = 'echovr-match::match.api.sent';
        } else {
            $repo = app(DaemonFileRepository::class)->setServer($server);
            $settings = self::file($server, 'evr-settings.json', true)['data'] ?? [];
            $settings[$key] = $on;
            $repo->putContent('evr-settings.json', json_encode($settings, JSON_PRETTY_PRINT | JSON_THROW_ON_ERROR) . "\n");
            $result = 'echovr-match::match.api.saved';
        }
        foreach (['status.json', 'evr-settings.json', 'match.json'] as $f) {
            Cache::forget("echovr-match.$server->uuid.$f");
        }

        return $result;
    }
}
