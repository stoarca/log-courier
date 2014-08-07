/*
 * Copyright 2014 Jason Woods.
 *
 * This file is a modification of code from Logstash Forwarder.
 * Copyright 2012-2013 Jordan Sissel and contributors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package spooler

import (
  "lc-lib/core"
  "lc-lib/publisher"
  "time"
)

type Spooler struct {
  core.PipelineSegment
  core.PipelineConfigReceiver
  core.PipelineSnapshotProvider

  config      *core.GeneralConfig
  spool       []*core.EventDescriptor
  input       chan *core.EventDescriptor
  output      chan<- []*core.EventDescriptor
  timer_start time.Time
  timer       *time.Timer
}

func NewSpooler(pipeline *core.Pipeline, config *core.GeneralConfig, publisher_imp *publisher.Publisher) *Spooler {
  ret := &Spooler{
    config: config,
    spool:  make([]*core.EventDescriptor, 0, config.SpoolSize),
    input:  make(chan *core.EventDescriptor, 16), // TODO: Make configurable?
    output: publisher_imp.Connect(),
  }

  pipeline.Register(ret)

  return ret
}

func (s *Spooler) Connect() chan<- *core.EventDescriptor {
  return s.input
}

func (s *Spooler) Run() {
  defer func() {
    s.Done()
  }()

  s.timer_start = time.Now()
  s.timer = time.NewTimer(s.config.SpoolTimeout)

SpoolerLoop:
  for {
    select {
    case event := <-s.input:
      s.spool = append(s.spool, event)

      // Flush if full
      if len(s.spool) == cap(s.spool) {
        if !s.sendSpool() {
          break SpoolerLoop
        }
        s.timer_start = time.Now()
        s.timer.Reset(s.config.SpoolTimeout)
      }
    case <-s.timer.C:
      // Flush what we have, if anything
      if len(s.spool) > 0 {
        if !s.sendSpool() {
          break SpoolerLoop
        }
      }

      s.timer_start = time.Now()
      s.timer.Reset(s.config.SpoolTimeout)
    case <-s.OnShutdown():
      break SpoolerLoop
    case <-s.OnSnapshot():
      s.handleSnapshot()
    case config := <-s.OnConfig():
      if !s.reloadConfig(config) {
        break SpoolerLoop
      }
    }
  }

  log.Info("Spooler exiting")
}

func (s *Spooler) sendSpool() bool {
  select {
  case <-s.OnShutdown():
    return false
  case <-s.OnSnapshot():
    s.handleSnapshot()
  case config := <-s.OnConfig():
    if !s.reloadConfig(config) {
      return false
    }
  case s.output <- s.spool:
  }

  s.spool = make([]*core.EventDescriptor, 0, s.config.SpoolSize)

  return true
}

func (s *Spooler) reloadConfig(config *core.Config) bool {
  s.config = &config.General

  // Immediate flush?
  passed := time.Now().Sub(s.timer_start)
  if passed >= s.config.SpoolTimeout || len(s.spool) >= int(s.config.SpoolSize) {
    if !s.sendSpool() {
      return false
    }
    s.timer_start = time.Now()
    s.timer.Reset(s.config.SpoolTimeout)
  } else {
    s.timer.Reset(passed - s.config.SpoolTimeout)
  }

  return true
}

func (s *Spooler) handleSnapshot() {
  s.SendSnapshot()
}
