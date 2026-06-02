package config

import "testing"

func TestServerConfigDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.setServerDefaults()

	if cfg.Server.Port != 8888 {
		t.Errorf("expected default port 8888, got %d", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeoutSec != 30 {
		t.Errorf("expected ReadTimeoutSec 30, got %d", cfg.Server.ReadTimeoutSec)
	}
	if cfg.Server.WriteTimeoutSec != 600 {
		t.Errorf("expected WriteTimeoutSec 600, got %d", cfg.Server.WriteTimeoutSec)
	}
	if cfg.Server.IdleTimeoutSec != 120 {
		t.Errorf("expected IdleTimeoutSec 120, got %d", cfg.Server.IdleTimeoutSec)
	}
	if cfg.Server.ShutdownTimeoutSec != 10 {
		t.Errorf("expected ShutdownTimeoutSec 10, got %d", cfg.Server.ShutdownTimeoutSec)
	}
	if cfg.Server.RateLimitPerSec != 100 {
		t.Errorf("expected RateLimitPerSec 100, got %d", cfg.Server.RateLimitPerSec)
	}
}

func TestKafkaInternalDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.setKafkaInternalDefaults()

	if cfg.Kafka.WorkerPoolSize != 10 {
		t.Errorf("expected WorkerPoolSize 10, got %d", cfg.Kafka.WorkerPoolSize)
	}
	if cfg.Kafka.TaskChanBuffer != 1000 {
		t.Errorf("expected TaskChanBuffer 1000, got %d", cfg.Kafka.TaskChanBuffer)
	}
	if cfg.Kafka.MaxBackoffMs != 30000 {
		t.Errorf("expected MaxBackoffMs 30000, got %d", cfg.Kafka.MaxBackoffMs)
	}
	if cfg.Kafka.PublishTimeoutSec != 60 {
		t.Errorf("expected PublishTimeoutSec 60, got %d", cfg.Kafka.PublishTimeoutSec)
	}
	if cfg.Kafka.KafkaSemaphore != 50 {
		t.Errorf("expected KafkaSemaphore 50, got %d", cfg.Kafka.KafkaSemaphore)
	}
	if cfg.Kafka.BizPublishChanBuffer != 500 {
		t.Errorf("expected BizPublishChanBuffer 500, got %d", cfg.Kafka.BizPublishChanBuffer)
	}
}

func TestExecutorInternalDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.setExecutorInternalDefaults()

	if cfg.Executor.DefaultNamespace != "default" {
		t.Errorf("expected DefaultNamespace 'default', got '%s'", cfg.Executor.DefaultNamespace)
	}
	if cfg.Executor.MaxRestartCount != 5 {
		t.Errorf("expected MaxRestartCount 5, got %d", cfg.Executor.MaxRestartCount)
	}
	if cfg.Executor.CoolDownMinutes != 5 {
		t.Errorf("expected CoolDownMinutes 5, got %d", cfg.Executor.CoolDownMinutes)
	}
	if cfg.Executor.GracePeriodSec != 30 {
		t.Errorf("expected GracePeriodSec 30, got %d", cfg.Executor.GracePeriodSec)
	}
	if cfg.Executor.DeleteGracePeriodSec != 5 {
		t.Errorf("expected DeleteGracePeriodSec 5, got %d", cfg.Executor.DeleteGracePeriodSec)
	}
	if cfg.Executor.MaxReplicas != 100 {
		t.Errorf("expected MaxReplicas 100, got %d", cfg.Executor.MaxReplicas)
	}
	if cfg.Executor.DefaultHPA.MinReplicas != 1 {
		t.Errorf("expected MinReplicas 1, got %d", cfg.Executor.DefaultHPA.MinReplicas)
	}
	if cfg.Executor.DefaultHPA.MaxReplicas != 5 {
		t.Errorf("expected MaxReplicas 5, got %d", cfg.Executor.DefaultHPA.MaxReplicas)
	}
	if cfg.Executor.DefaultHPA.TargetCPU != 50 {
		t.Errorf("expected TargetCPU 50, got %d", cfg.Executor.DefaultHPA.TargetCPU)
	}
}

func TestServerConfigPreservesOverrides(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Port = 9999
	cfg.Server.ReadTimeoutSec = 15
	cfg.setServerDefaults()

	if cfg.Server.Port != 9999 {
		t.Errorf("expected overridden port 9999, got %d", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeoutSec != 15 {
		t.Errorf("expected overridden ReadTimeoutSec 15, got %d", cfg.Server.ReadTimeoutSec)
	}
	if cfg.Server.RateLimitPerSec != 100 {
		t.Errorf("expected default RateLimitPerSec 100, got %d", cfg.Server.RateLimitPerSec)
	}
}
